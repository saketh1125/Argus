package services

import (
	"fmt"
	"io"
	"net/http"
)

const maxServiceResponseBytes = 4 << 20

func readServiceResponse(resp *http.Response) ([]byte, error) {
	if resp.ContentLength > maxServiceResponseBytes {
		return nil, fmt.Errorf("response exceeds %d-byte limit", maxServiceResponseBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxServiceResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxServiceResponseBytes {
		return nil, fmt.Errorf("response exceeds %d-byte limit", maxServiceResponseBytes)
	}
	return data, nil
}
