//go:build windows

package platform

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const maxJobMembers = 65536

var isProcessInOwnedJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func currentJobMembers(handle windows.Handle, deadline time.Time) ([]uint32, error) {
	for capacity := 32; capacity <= maxJobMembers; capacity *= 2 {
		if !time.Now().Before(deadline) {
			return nil, ErrJobRetirementTimeout
		}
		storage := make([]uintptr, capacity+2)
		size := uint32(8 + capacity*int(unsafe.Sizeof(uintptr(0))))
		err := windows.QueryInformationJobObject(handle, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&storage[0])), size, nil)
		header := (*[2]uint32)(unsafe.Pointer(&storage[0]))
		if errors.Is(err, windows.ERROR_MORE_DATA) || (err == nil && header[1] < header[0]) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("query job members: %w", err)
		}
		if header[1] > uint32(capacity) {
			return nil, fmt.Errorf("job member list exceeds buffer")
		}
		ids := unsafe.Slice((*uintptr)(unsafe.Add(unsafe.Pointer(&storage[0]), 8)), int(header[1]))
		result := make([]uint32, len(ids))
		for i, id := range ids {
			if !time.Now().Before(deadline) {
				return nil, ErrJobRetirementTimeout
			}
			if id == 0 || uint64(id) > uint64(^uint32(0)) {
				return nil, fmt.Errorf("invalid job member identity %d", id)
			}
			result[i] = uint32(id)
		}
		return result, nil
	}
	return nil, fmt.Errorf("job member census exceeds %d members", maxJobMembers)
}

func (job *ownedJob) pinCurrentMembers(deadline time.Time) error {
	census := currentJobMembers
	if job.census != nil {
		census = job.census
	}
	ids, err := census(job.handle, deadline)
	if err != nil {
		return err
	}
	for _, pid := range ids {
		if !time.Now().Before(deadline) {
			return ErrJobRetirementTimeout
		}
		if _, known := job.members[pid]; known {
			continue
		}
		if len(job.members) >= maxJobMembers {
			return fmt.Errorf("retained job members exceed %d", maxJobMembers)
		}
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			return fmt.Errorf("pin job member %d: %w", pid, err)
		}
		if !time.Now().Before(deadline) {
			_ = windows.CloseHandle(handle)
			return ErrJobRetirementTimeout
		}
		var member int32
		ok, _, callErr := isProcessInOwnedJob.Call(uintptr(handle), uintptr(job.handle), uintptr(unsafe.Pointer(&member)))
		if ok == 0 {
			_ = windows.CloseHandle(handle)
			return fmt.Errorf("attest job member %d: membership=%d: %w", pid, member, callErr)
		}
		if member == 0 {
			_ = windows.CloseHandle(handle)
			return fmt.Errorf("process %d is not a member of the owned job", pid)
		}
		job.members[pid] = handle
	}
	return nil
}

func (job *ownedJob) retire(deadline time.Time) error {
	for {
		stable, err := job.allMembersSignaled(deadline)
		if err != nil {
			return err
		}
		previous := len(job.members)
		if err := job.pinCurrentMembers(deadline); err != nil {
			return err
		}
		if stable && previous == len(job.members) {
			if !time.Now().Before(deadline) {
				return ErrJobRetirementTimeout
			}
			job.retired = true
			return nil
		}
		for _, handle := range job.members {
			if !time.Now().Before(deadline) {
				return ErrJobRetirementTimeout
			}
			status, err := windows.WaitForSingleObject(handle, 0)
			if err != nil {
				return fmt.Errorf("probe job member: %w", err)
			}
			if status == windows.WAIT_OBJECT_0 {
				continue
			}
			if err := retireJobMember(handle, deadline); err != nil {
				return err
			}
		}
	}
}

// The final census must follow observed parent completion: probing only after
// a census could miss a child born while its parent was exiting naturally.
func (job *ownedJob) allMembersSignaled(deadline time.Time) (bool, error) {
	for _, handle := range job.members {
		if !time.Now().Before(deadline) {
			return false, ErrJobRetirementTimeout
		}
		status, err := windows.WaitForSingleObject(handle, 0)
		if err != nil {
			return false, fmt.Errorf("probe captured job member: %w", err)
		}
		if status != windows.WAIT_OBJECT_0 {
			return false, nil
		}
	}
	return true, nil
}

func retireJobMember(handle windows.Handle, deadline time.Time) error {
	if !time.Now().Before(deadline) {
		return ErrJobRetirementTimeout
	}
	if err := windows.TerminateProcess(handle, 1); err != nil {
		status, waitErr := windows.WaitForSingleObject(handle, 0)
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || waitErr != nil || status != windows.WAIT_OBJECT_0 {
			return fmt.Errorf("terminate pinned job member: %w", err)
		}
		return nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return ErrJobRetirementTimeout
	}
	millis := uint32((remaining + time.Millisecond - 1) / time.Millisecond)
	status, err := windows.WaitForSingleObject(handle, millis)
	if err != nil {
		return fmt.Errorf("wait pinned job member: %w", err)
	}
	if status != windows.WAIT_OBJECT_0 {
		return ErrJobRetirementTimeout
	}
	return nil
}
