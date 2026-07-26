package kafkaproducer

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"
)

func TestEncodeBase64GzipJSON(t *testing.T) {
	input := []byte(`{"BetMessages":[{"BetId":"my-bet-001","ActualStake":400,"BrandProviderId":1,"CustomerId":1,"BetStatus":5,"SettledAt":"2026-05-15T12:00:00Z","IsReSettled":false}]}`)
	encoded, err := EncodeBase64GzipJSON(input)
	if err != nil {
		t.Fatalf("EncodeBase64GzipJSON: %v", err)
	}
	payload := decodeBase64GzipJSON(t, encoded)
	assertJSONEqual(t, payload, input)
}

func TestEncodeBase64GzipJSONPreservesIntegerNumbers(t *testing.T) {
	input := []byte(`{"BetMessages":[{"ActualStake":400,"BrandProviderId":1,"CustomerId":1,"BetStatus":5}]}`)
	encoded, err := EncodeBase64GzipJSON(input)
	if err != nil {
		t.Fatalf("EncodeBase64GzipJSON: %v", err)
	}
	payload := decodeBase64GzipJSON(t, encoded)
	for _, want := range []string{`"ActualStake":400`, `"BrandProviderId":1`, `"CustomerId":1`, `"BetStatus":5`} {
		if !bytes.Contains(payload, []byte(want)) {
			t.Fatalf("payload = %s, missing %s", payload, want)
		}
	}
}

func TestEncodeBase64GzipJSONRejectsInvalidJSON(t *testing.T) {
	if _, err := EncodeBase64GzipJSON([]byte(`{"broken"`)); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func decodeBase64GzipJSON(t *testing.T, encoded []byte) []byte {
	t.Helper()
	compressed, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer func() { _ = gz.Close() }()
	payload, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	return payload
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	gotJSON, _ := json.Marshal(gotValue)
	wantJSON, _ := json.Marshal(wantValue)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("json mismatch\ngot:  %s\nwant: %s", got, want)
	}
}
