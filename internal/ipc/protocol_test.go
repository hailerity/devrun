package ipc_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFraming_RoundTrip(t *testing.T) {
	req := ipc.Request{Type: "start", Payload: json.RawMessage(`{"name":"web"}`)}
	var buf bytes.Buffer
	require.NoError(t, ipc.WriteMessage(&buf, req))

	var decoded ipc.Request
	require.NoError(t, ipc.ReadMessage(&buf, &decoded))
	assert.Equal(t, "start", decoded.Type)
	assert.JSONEq(t, `{"name":"web"}`, string(decoded.Payload))
}

func TestFraming_ResponseError(t *testing.T) {
	resp := ipc.Response{OK: false, Error: "service not found"}
	var buf bytes.Buffer
	require.NoError(t, ipc.WriteMessage(&buf, resp))

	var decoded ipc.Response
	require.NoError(t, ipc.ReadMessage(&buf, &decoded))
	assert.False(t, decoded.OK)
	assert.Equal(t, "service not found", decoded.Error)
}

func TestFraming_MultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < 3; i++ {
		require.NoError(t, ipc.WriteMessage(&buf, ipc.Response{OK: true}))
	}
	for i := 0; i < 3; i++ {
		var resp ipc.Response
		require.NoError(t, ipc.ReadMessage(&buf, &resp))
		assert.True(t, resp.OK)
	}
}

func TestFraming_EOFFrame(t *testing.T) {
	eof := ipc.Response{OK: true, EOF: true}
	var buf bytes.Buffer
	require.NoError(t, ipc.WriteMessage(&buf, eof))
	var decoded ipc.Response
	require.NoError(t, ipc.ReadMessage(&buf, &decoded))
	assert.True(t, decoded.EOF)
}
