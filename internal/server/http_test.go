package server_test

import (
	"bytes"
	"distributed-systems/internal/server"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLog(t *testing.T) {
	// Arrange
	r := server.New(server.NewLog()).HandleLog("/")
	srv := httptest.NewServer(r)
	defer srv.Close()
	c := srv.Client()

	// Act: Post data
	buf := bytes.NewBufferString(`{"record": {"value": "aGVsbG8="}}`)
	resp, err := c.Post(srv.URL, "application/json", buf)

	// Assert: Check if the response is OK
	if err != nil {
		t.Error(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d; want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	// Act: Get data
	resp, err = c.Get(srv.URL + "?offset=0")
	if err != nil {
		t.Error(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d; want %d", resp.StatusCode, http.StatusOK)
	}
	var got server.ConsumeResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Error(err)
	}
	// aGVsbG8= is base64 for "hello"
	if string(got.Value) != "hello" {
		t.Errorf("got %q; want %q", got.Value, "hello")
	}
	resp.Body.Close()
}
