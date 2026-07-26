package kafkaproducer

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// EncodeBase64GzipJSON converts the request value to the mgmt-compatible payload:
// Base64(GZip(JSON)).
func EncodeBase64GzipJSON(input []byte) ([]byte, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode value json: %w", err)
	}
	jsonPayload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode value json: %w", err)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(jsonPayload); err != nil {
		_ = gz.Close()
		return nil, fmt.Errorf("gzip value json: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(compressed.Bytes())
	return []byte(encoded), nil
}
