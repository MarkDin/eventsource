package eventsource

import (
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		rawInput     string
		wantedEvents []*publication
	}{
		{
			rawInput:     "event: eventName\ndata: {\"sample\":\"value\"}\n\n",
			wantedEvents: []*publication{{event: "eventName", data: "{\"sample\":\"value\"}"}},
		},
		{
			// the newlines should not be parsed as empty event
			rawInput:     "\n\n\nevent: event1\n\n\n\n\nevent: event2\n\n",
			wantedEvents: []*publication{{event: "event1"}, {event: "event2"}},
		},
		{
			rawInput:     "id: abc\ndata: def\n\n",
			wantedEvents: []*publication{{id: "abc", lastEventID: "abc", data: "def"}},
		},
		{
			// id field should be ignored if it contains a null
			rawInput:     "id: a\x00bc\ndata: def\n\n",
			wantedEvents: []*publication{{data: "def"}},
		},
	}

	for _, test := range tests {
		decoder := NewDecoder(strings.NewReader(test.rawInput))
		i := 0
		for {
			event, err := decoder.Decode()
			if err == io.EOF {
				break
			}
			require.NoError(t, err, "for input: %q", test.rawInput)
			assert.Equal(t, test.wantedEvents[i], event, "for input: %q", test.rawInput)
			i++
		}
		assert.Equal(t, len(test.wantedEvents), i, "Wrong number of decoded events")
	}
}

func requireLastEventID(t *testing.T, event Event) string {
	// necessary because we can't yet add LastEventID to the basic Event interface; see EventWithLastID
	eventWithID, ok := event.(EventWithLastID)
	require.True(t, ok, "event should have implemented EventWithLastID")
	return eventWithID.LastEventID()
}

func TestDecoderTracksLastEventID(t *testing.T) {
	t.Run("uses last ID that is passed in options", func(t *testing.T) {
		inputData := "data: abc\n\n"
		decoder := NewDecoderWithOptions(strings.NewReader(inputData), DecoderOptionLastEventID("my-id"))

		event, err := decoder.Decode()
		require.NoError(t, err)

		assert.Equal(t, "abc", event.Data())
		assert.Equal(t, "", event.Id())
		assert.Equal(t, "my-id", requireLastEventID(t, event))
	})

	t.Run("last ID persists if not overridden", func(t *testing.T) {
		inputData := "id: abc\ndata: first\n\ndata: second\n\nid: def\ndata:third\n\n"
		decoder := NewDecoderWithOptions(strings.NewReader(inputData), DecoderOptionLastEventID("my-id"))

		event1, err := decoder.Decode()
		require.NoError(t, err)

		assert.Equal(t, "first", event1.Data())
		assert.Equal(t, "abc", event1.Id())
		assert.Equal(t, "abc", requireLastEventID(t, event1))

		event2, err := decoder.Decode()
		require.NoError(t, err)

		assert.Equal(t, "second", event2.Data())
		assert.Equal(t, "", event2.Id())
		assert.Equal(t, "abc", requireLastEventID(t, event2))

		event3, err := decoder.Decode()
		require.NoError(t, err)

		assert.Equal(t, "third", event3.Data())
		assert.Equal(t, "def", event3.Id())
		assert.Equal(t, "def", requireLastEventID(t, event3))
	})

	t.Run("last ID persists if not overridden", func(t *testing.T) {
		inputData := "id: abc\ndata: first\n\ndata: second\n\nid: def\ndata:third\n\n"
		decoder := NewDecoderWithOptions(strings.NewReader(inputData), DecoderOptionLastEventID("my-id"))

		event1, err := decoder.Decode()
		require.NoError(t, err)

		assert.Equal(t, "first", event1.Data())
		assert.Equal(t, "abc", event1.Id())
		assert.Equal(t, "abc", requireLastEventID(t, event1))

		event2, err := decoder.Decode()
		require.NoError(t, err)

		assert.Equal(t, "second", event2.Data())
		assert.Equal(t, "", event2.Id())
		assert.Equal(t, "abc", requireLastEventID(t, event2))

		event3, err := decoder.Decode()
		require.NoError(t, err)

		assert.Equal(t, "third", event3.Data())
		assert.Equal(t, "def", event3.Id())
		assert.Equal(t, "def", requireLastEventID(t, event3))
	})

	t.Run("last ID can be overridden with empty string", func(t *testing.T) {
		inputData := "id: abc\ndata: first\n\nid: \ndata: second\n\n"
		decoder := NewDecoderWithOptions(strings.NewReader(inputData), DecoderOptionLastEventID("my-id"))

		event1, err := decoder.Decode()
		require.NoError(t, err)

		assert.Equal(t, "first", event1.Data())
		assert.Equal(t, "abc", event1.Id())
		assert.Equal(t, "abc", requireLastEventID(t, event1))

		event2, err := decoder.Decode()
		require.NoError(t, err)

		assert.Equal(t, "second", event2.Data())
		assert.Equal(t, "", event2.Id())
		assert.Equal(t, "", requireLastEventID(t, event2))
	})
}

func TestGoroutineCleanup(t *testing.T) {
	before := runtime.NumGoroutine()

	reader := strings.NewReader("data: test\n\n")
	decoder := NewDecoder(reader)

	// 读取一个事件
	event, err := decoder.Decode()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(event)
	// 关闭 decoder
	decoder.Close()
	// decoder.Decode()
	// 等待一小段时间确保 goroutine 都已退出
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before {
		t.Errorf("Goroutine leak detected: before=%d, after=%d", before, after)
	}
}

func TestEOFOnClose(t *testing.T) {
	num := runtime.NumGoroutine()
	reader := strings.NewReader("data: test\n\n")
	decoder := NewDecoder(reader)

	// 在另一个 goroutine 中关闭 decoder
	go func() {
		// time.Sleep(100 * time.Millisecond)
		decoder.Close()
	}()
	time.Sleep(100 * time.Millisecond)
	// 这次读取应该返回 EOF
	_, err := decoder.Decode()
	if err != io.EOF {
		t.Errorf("Expected EOF error, got: %v", err)
	}
	t.Log(runtime.NumGoroutine(), num)
	// t.Log(decoder.Decode())
}
