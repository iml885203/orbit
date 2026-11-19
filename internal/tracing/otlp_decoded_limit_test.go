package tracing

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOTLPDecodedExportCeiling(t *testing.T) {
	for _, path := range []string{"/v1/traces", "/v1/logs"} {
		for _, encoding := range []string{"application/json", "application/x-protobuf"} {
			for _, compressed := range []bool{false, true} {
				t.Run(path+"/"+encoding+map[bool]string{false: "/plain", true: "/gzip"}[compressed], func(t *testing.T) {
					for _, extra := range []int{0, 1} {
						// JSON whitespace and protobuf unknown length-delimited fields make
						// valid exports at the exact decoded boundary without retained spans.
						var body []byte
						if encoding == "application/json" {
							body = []byte("{}" + strings.Repeat(" ", maxOTLPBodyBytes-2+extra))
						} else {
							body = append([]byte{0x7a, 0xfb, 0xff, 0xff, 0x03}, make([]byte, maxOTLPBodyBytes-5+extra)...)
							if extra == 1 {
								body[1] = 0xfc
							}
						}
						if compressed {
							var zipped bytes.Buffer
							zw := gzip.NewWriter(&zipped)
							if _, err := zw.Write(body); err != nil {
								t.Fatal(err)
							}
							if err := zw.Close(); err != nil {
								t.Fatal(err)
							}
							body = zipped.Bytes()
						}
						store := NewStore(10)
						r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
						r.Header.Set("Content-Type", encoding)
						if compressed {
							r.Header.Set("Content-Encoding", "gzip")
						}
						out := httptest.NewRecorder()
						store.OTLPHandler().ServeHTTP(out, r)
						want := http.StatusOK
						if extra > 0 {
							want = http.StatusRequestEntityTooLarge
						}
						if out.Code != want {
							t.Fatalf("extra=%d: status=%d want %d: %s", extra, out.Code, want, out.Body.String())
						}
						if store.Stats().TotalSpans != 0 || len(store.Logs().records) != 0 {
							t.Fatal("boundary export ingested records")
						}
					}
				})
			}
		}
	}
}

func TestOTLPMalformedExportsDoNotIngest(t *testing.T) {
	var zipped bytes.Buffer
	writer := gzip.NewWriter(&zipped)
	if _, err := writer.Write([]byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	valid := zipped.Bytes()
	broken := bytes.Clone(valid)
	broken[len(broken)-8] ^= 0xff
	for _, path := range []string{"/v1/traces", "/v1/logs"} {
		for _, body := range [][]byte{[]byte("bad gzip"), valid[:len(valid)-2], broken} {
			s := NewStore(10)
			r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			r.Header.Set("Content-Encoding", "gzip")
			r.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			s.OTLPHandler().ServeHTTP(out, r)
			if out.Code != http.StatusBadRequest || s.Stats().TotalSpans != 0 || len(s.Logs().records) != 0 {
				t.Fatalf("status=%d; malformed input ingested", out.Code)
			}
		}
		for _, encoding := range []string{"application/json", "application/x-protobuf"} {
			s := NewStore(10)
			r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte{0xff}))
			r.Header.Set("Content-Type", encoding)
			out := httptest.NewRecorder()
			s.OTLPHandler().ServeHTTP(out, r)
			if out.Code != http.StatusBadRequest {
				t.Fatalf("malformed %s status=%d", encoding, out.Code)
			}
		}
	}
}
