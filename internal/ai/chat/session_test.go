package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionStore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "session-store-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	store, err := NewSessionStore(tempDir)
	require.NoError(t, err)

	t.Run("Create and Get", func(t *testing.T) {
		session, err := store.Create()
		require.NoError(t, err)
		assert.NotEmpty(t, session.ID)
		assert.Empty(t, session.Title)

		retrieved, err := store.Get(session.ID)
		require.NoError(t, err)
		assert.Equal(t, session.ID, retrieved.ID)
	})

	t.Run("List", func(t *testing.T) {
		// New store for isolation
		d := filepath.Join(tempDir, "list-test")
		s, _ := NewSessionStore(d)

		s1, _ := s.Create()
		time.Sleep(10 * time.Millisecond) // Ensure time difference
		s2, _ := s.Create()

		sessions, err := s.List()
		require.NoError(t, err)
		require.Len(t, sessions, 2)
		// Should be newest first
		assert.Equal(t, s2.ID, sessions[0].ID)
		assert.Equal(t, s1.ID, sessions[1].ID)
	})

	t.Run("Delete", func(t *testing.T) {
		session, _ := store.Create()
		err := store.Delete(session.ID)
		assert.NoError(t, err)

		_, err = store.Get(session.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session not found")
	})

	t.Run("AddMessage and Title Generation", func(t *testing.T) {
		session, _ := store.Create()
		msg := Message{
			Role:    "user",
			Content: "What is the status of node-1?",
		}
		err := store.AddMessage(session.ID, msg)
		require.NoError(t, err)

		updated, _ := store.Get(session.ID)
		assert.Equal(t, "What is the status of node-1?", updated.Title)
		assert.Equal(t, 1, updated.MessageCount)

		messages, err := store.GetMessages(session.ID)
		require.NoError(t, err)
		require.Len(t, messages, 1)
		assert.Equal(t, "What is the status of node-1?", messages[0].Content)
	})

	t.Run("UpdateLastMessage", func(t *testing.T) {
		session, _ := store.Create()
		store.AddMessage(session.ID, Message{Role: "assistant", Content: "Thinking..."})

		updatedMsg := Message{Role: "assistant", Content: "Resolved."}
		err := store.UpdateLastMessage(session.ID, updatedMsg)
		require.NoError(t, err)

		messages, _ := store.GetMessages(session.ID)
		assert.Equal(t, "Resolved.", messages[0].Content)
	})

	t.Run("EnsureSession", func(t *testing.T) {
		session, err := store.EnsureSession("")
		assert.NoError(t, err)
		assert.NotEmpty(t, session.ID)

		sessionFixed, err := store.EnsureSession("fixed-id")
		assert.NoError(t, err)
		assert.Equal(t, "fixed-id", sessionFixed.ID)

		retrieved, err := store.EnsureSession("fixed-id")
		assert.NoError(t, err)
		assert.Equal(t, sessionFixed.ID, retrieved.ID)
	})
}

func TestGenerateTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Short message", "Short message"},
		{"This is a very long message that should definitely be truncated because it exceeds the fifty character limit", "This is a very long message that should..."},
		{"Multiple    spaces    cleaned", "Multiple spaces cleaned"},
		{"New\nLines\nRemoved", "New Lines Removed"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, generateTitle(tt.input))
	}
}

func TestSessionStore_HashedPathsAndLegacyCompatibility(t *testing.T) {
	store, err := NewSessionStore(t.TempDir())
	require.NoError(t, err)

	session := sessionData{
		ID:        "legacy-session",
		Title:     "Legacy Title",
		Messages:  []Message{{Role: "user", Content: "hello"}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	path := store.sessionPath(session.ID)
	assert.Equal(t, filepath.Join(store.dataDir, hashedSessionStorageName(session.ID)+".json"), path)
	assert.NotContains(t, filepath.Base(path), "..")

	legacyPath := store.legacySessionPath(session.ID)
	raw, err := json.Marshal(session)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(legacyPath, raw, 0600))

	got, err := store.Get(session.ID)
	require.NoError(t, err)
	assert.Equal(t, session.ID, got.ID)
	assert.Equal(t, session.Title, got.Title)

	sessions, err := store.List()
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, session.ID, sessions[0].ID)
}

func TestSessionStore_PathTraversalIDsStayWithinStore(t *testing.T) {
	store, err := NewSessionStore(t.TempDir())
	require.NoError(t, err)

	err = store.writeSession(sessionData{
		ID:        "..",
		Title:     "Traversal",
		Messages:  []Message{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	require.NoError(t, err)

	path := store.sessionPath("..")
	rel, err := filepath.Rel(store.dataDir, path)
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(rel, ".."))

	got, err := store.Get("..")
	require.NoError(t, err)
	assert.Equal(t, "..", got.ID)

	require.NoError(t, store.Delete(".."))
	_, err = store.Get("..")
	assert.Error(t, err)
}
