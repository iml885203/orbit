package container

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestImagePullCoordinatorSharesSameImagePlatform(t *testing.T) {
	coordinator := newImagePullCoordinator()
	key := newImagePullKey("redis", "LINUX/AMD64")
	pullStarted := make(chan struct{})
	allowPull := make(chan struct{})
	waiting := make(chan struct{})
	var pulls atomic.Int32

	first := make(chan error, 1)
	go func() {
		first <- coordinator.Do(context.Background(), key, imagePullHooks{}, func(context.Context) error {
			pulls.Add(1)
			close(pullStarted)
			<-allowPull
			return nil
		})
	}()
	<-pullStarted

	second := make(chan error, 1)
	go func() {
		second <- coordinator.Do(context.Background(), newImagePullKey("docker.io/library/redis:latest", "linux/amd64"), imagePullHooks{
			Waiting: func() { close(waiting) },
		}, func(context.Context) error {
			pulls.Add(1)
			return nil
		})
	}()
	<-waiting

	select {
	case err := <-second:
		t.Fatalf("follower returned before image was ready: %v", err)
	default:
	}
	close(allowPull)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if got := pulls.Load(); got != 1 {
		t.Fatalf("pull count = %d, want 1", got)
	}
}

func TestImagePullCoordinatorKeepsPlatformsIndependent(t *testing.T) {
	coordinator := newImagePullCoordinator()
	started := make(chan string, 2)
	release := make(chan struct{})

	for _, platform := range []string{"linux/amd64", "linux/arm64"} {
		platform := platform
		go func() {
			_ = coordinator.Do(context.Background(), newImagePullKey("redis:7", platform), imagePullHooks{}, func(context.Context) error {
				started <- platform
				<-release
				return nil
			})
		}()
	}

	want := map[string]bool{"linux/amd64": true, "linux/arm64": true}
	for range 2 {
		select {
		case platform := <-started:
			delete(want, platform)
		case <-time.After(time.Second):
			t.Fatal("distinct platforms did not pull concurrently")
		}
	}
	close(release)
	if len(want) != 0 {
		t.Fatalf("platforms not started: %v", want)
	}
}

func TestImagePullCoordinatorAllowsDistinctImagesWithoutLimit(t *testing.T) {
	coordinator := newImagePullCoordinator()
	started := make(chan string, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for _, image := range []string{"redis:7", "mongo:7"} {
		image := image
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = coordinator.Do(context.Background(), newImagePullKey(image, ""), imagePullHooks{}, func(context.Context) error {
				started <- image
				<-release
				return nil
			})
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("limit 0 did not allow distinct image pulls to overlap")
		}
	}
	close(release)
	wg.Wait()
}

func TestImagePullCoordinatorLimitsDistinctPullsWithoutBarrier(t *testing.T) {
	coordinator := newImagePullCoordinator()
	coordinator.SetLimit(1)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	queued := make(chan struct{})

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- coordinator.Do(context.Background(), newImagePullKey("redis:7", ""), imagePullHooks{}, func(context.Context) error {
			close(firstStarted)
			<-releaseFirst
			return nil
		})
	}()
	<-firstStarted

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- coordinator.Do(context.Background(), newImagePullKey("mongo:7", ""), imagePullHooks{
			Queued: func() { close(queued) },
		}, func(context.Context) error {
			close(secondStarted)
			<-releaseSecond
			return nil
		})
	}()
	<-queued
	select {
	case <-secondStarted:
		t.Fatal("second pull started before the first released its slot")
	default:
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("queued pull did not start after slot became available")
	}
	select {
	case err := <-secondDone:
		t.Fatalf("second pull completed before its own image was ready: %v", err)
	default:
	}
	close(releaseSecond)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestImagePullCoordinatorHonorsLimitGreaterThanOne(t *testing.T) {
	coordinator := newImagePullCoordinator()
	coordinator.SetLimit(2)
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	var wg sync.WaitGroup

	for _, image := range []string{"one:latest", "two:latest", "three:latest", "four:latest"} {
		image := image
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = coordinator.Do(context.Background(), newImagePullKey(image, ""), imagePullHooks{}, func(context.Context) error {
				current := active.Add(1)
				for {
					observed := maximum.Load()
					if current <= observed || maximum.CompareAndSwap(observed, current) {
						break
					}
				}
				<-release
				active.Add(-1)
				return nil
			})
		}()
	}

	deadline := time.Now().Add(time.Second)
	for maximum.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum active pulls = %d, want 2", got)
	}
	close(release)
	wg.Wait()
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum active pulls = %d, want 2", got)
	}
}

func TestImagePullCoordinatorSharesFailureAndAllowsRetry(t *testing.T) {
	coordinator := newImagePullCoordinator()
	key := newImagePullKey("redis:7", "")
	pullStarted := make(chan struct{})
	release := make(chan struct{})
	waiting := make(chan struct{})
	wantErr := errors.New("registry unavailable")

	first := make(chan error, 1)
	go func() {
		first <- coordinator.Do(context.Background(), key, imagePullHooks{}, func(context.Context) error {
			close(pullStarted)
			<-release
			return wantErr
		})
	}()
	<-pullStarted
	second := make(chan error, 1)
	go func() {
		second <- coordinator.Do(context.Background(), key, imagePullHooks{
			Waiting: func() { close(waiting) },
		}, func(context.Context) error { return nil })
	}()
	<-waiting
	close(release)
	if err := <-first; !errors.Is(err, wantErr) {
		t.Fatalf("leader error = %v", err)
	}
	if err := <-second; !errors.Is(err, wantErr) {
		t.Fatalf("follower error = %v", err)
	}

	retried := false
	if err := coordinator.Do(context.Background(), key, imagePullHooks{}, func(context.Context) error {
		retried = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !retried {
		t.Fatal("failed pull remained cached instead of retrying")
	}
}

func TestImagePullCoordinatorKeepsSharedPullAliveForRemainingWaiter(t *testing.T) {
	coordinator := newImagePullCoordinator()
	key := newImagePullKey("redis:7", "")
	pullStarted := make(chan struct{})
	releasePull := make(chan struct{})
	leaderCtx, cancelLeader := context.WithCancel(context.Background())

	leader := make(chan error, 1)
	go func() {
		leader <- coordinator.Do(leaderCtx, key, imagePullHooks{}, func(ctx context.Context) error {
			close(pullStarted)
			select {
			case <-releasePull:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	<-pullStarted

	follower := make(chan error, 1)
	joined := make(chan struct{})
	var duplicatePull atomic.Bool
	go func() {
		follower <- coordinator.Do(context.Background(), key, imagePullHooks{
			Waiting: func() { close(joined) },
		}, func(context.Context) error {
			duplicatePull.Store(true)
			return nil
		})
	}()
	<-joined
	cancelLeader()
	if err := <-leader; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want context canceled", err)
	}
	select {
	case err := <-follower:
		t.Fatalf("leader cancellation ended the follower's pull: %v", err)
	default:
	}
	close(releasePull)
	if err := <-follower; err != nil {
		t.Fatal(err)
	}
	if duplicatePull.Load() {
		t.Fatal("follower started a duplicate pull")
	}
}

func TestImagePullCoordinatorCancelsPullAfterAllWaitersLeave(t *testing.T) {
	coordinator := newImagePullCoordinator()
	key := newImagePullKey("redis:7", "")
	pullStarted := make(chan struct{})
	pullCanceled := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- coordinator.Do(ctx, key, imagePullHooks{}, func(ctx context.Context) error {
			close(pullStarted)
			<-ctx.Done()
			close(pullCanceled)
			return ctx.Err()
		})
	}()
	<-pullStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter error = %v, want context canceled", err)
	}
	select {
	case <-pullCanceled:
	case <-time.After(time.Second):
		t.Fatal("shared pull remained alive after its last waiter left")
	}

	retried := false
	if err := coordinator.Do(context.Background(), key, imagePullHooks{}, func(context.Context) error {
		retried = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !retried {
		t.Fatal("canceled pull prevented a new request from retrying")
	}
}
