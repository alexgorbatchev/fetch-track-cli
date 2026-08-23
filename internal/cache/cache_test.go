package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type TestData struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestCache_GetPut(t *testing.T) {
	tempDir := t.TempDir()
	c := NewInDir(tempDir, true)

	if !c.Enabled() {
		t.Fatal("expected cache to be enabled")
	}

	key := "test_key"
	namespace := "test_ns"
	original := TestData{Name: "fetch-track", Count: 42}

	// Initial Get (miss)
	var fetched TestData
	if c.Get(namespace, key, &fetched) {
		t.Fatal("expected cache miss on empty cache")
	}

	// Put
	if err := c.Put(namespace, key, original, time.Hour); err != nil {
		t.Fatalf("failed to Put item: %v", err)
	}

	// Get (hit)
	if !c.Get(namespace, key, &fetched) {
		t.Fatal("expected cache hit after Put")
	}

	if fetched.Name != original.Name || fetched.Count != original.Count {
		t.Errorf("fetched data %+v != original %+v", fetched, original)
	}
}

func TestCache_ExpiredTTL(t *testing.T) {
	tempDir := t.TempDir()
	c := NewInDir(tempDir, true)

	key := "expired_key"
	namespace := "test_ns"
	data := TestData{Name: "old_data", Count: 1}

	if err := c.Put(namespace, key, data, time.Millisecond); err != nil {
		t.Fatalf("failed to Put item: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	var fetched TestData
	if c.Get(namespace, key, &fetched) {
		t.Error("expected cache miss on expired TTL")
	}
}

func TestCache_GetFile_Expired(t *testing.T) {
	tempDir := t.TempDir()
	c := NewInDir(tempDir, true)

	namespace := "artworks"
	key := "expired_art"
	imageData := []byte{0x01, 0x02, 0x03}

	path, err := c.PutFile(namespace, key, imageData)
	if err != nil {
		t.Fatalf("PutFile failed: %v", err)
	}

	// Artificially change file modification time to 2 hours ago
	oldTime := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(path, oldTime, oldTime)

	_, found := c.GetFile(namespace, key, time.Hour)
	if found {
		t.Error("expected GetFile to return false for expired artwork file")
	}
}

func TestCache_InvalidOuterJSON(t *testing.T) {
	tempDir := t.TempDir()
	c := NewInDir(tempDir, true)

	namespace := "bad_outer"
	key := "corrupt"

	dir := filepath.Join(tempDir, namespace)
	_ = os.MkdirAll(dir, 0755)
	filePath := filepath.Join(dir, KeyHash(namespace, key)+".json")
	_ = os.WriteFile(filePath, []byte("invalid json data"), 0644)

	var data TestData
	if c.Get(namespace, key, &data) {
		t.Error("expected Get to return false on corrupt JSON file")
	}
}

func TestCache_InnerTargetUnmarshalError(t *testing.T) {
	tempDir := t.TempDir()
	c := NewInDir(tempDir, true)

	namespace := "mismatch_ns"
	key := "mismatch_key"

	entry := Entry{
		CreatedAt: time.Now(),
		TTL:       time.Hour,
		Data:      json.RawMessage(`"string_data"`),
	}
	entryData, _ := json.Marshal(entry)

	dir := filepath.Join(tempDir, namespace)
	_ = os.MkdirAll(dir, 0755)
	filePath := filepath.Join(dir, KeyHash(namespace, key)+".json")
	_ = os.WriteFile(filePath, entryData, 0644)

	var data TestData
	if c.Get(namespace, key, &data) {
		t.Error("expected Get to return false on inner target unmarshal mismatch")
	}
}

func TestCache_GetPutFile(t *testing.T) {
	tempDir := t.TempDir()
	c := NewInDir(tempDir, true)

	namespace := "artworks"
	key := "cover_art_url_123"
	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}

	path, err := c.PutFile(namespace, key, imageData)
	if err != nil {
		t.Fatalf("PutFile failed: %v", err)
	}

	readPath, found := c.GetFile(namespace, key, time.Hour)
	if !found {
		t.Fatal("GetFile expected to find cached image")
	}

	if readPath != path {
		t.Errorf("GetFile path %q != PutFile path %q", readPath, path)
	}

	readData, err := os.ReadFile(readPath)
	if err != nil {
		t.Fatalf("failed to read cached image file: %v", err)
	}

	if string(readData) != string(imageData) {
		t.Error("cached image binary content mismatch")
	}
}

func TestCache_PutFileDirError(t *testing.T) {
	tempDir := t.TempDir()
	c := NewInDir(tempDir, true)

	// Create a file at namespace position to force MkdirAll error
	nsPath := filepath.Join(tempDir, "file_as_ns")
	_ = os.WriteFile(nsPath, []byte("file"), 0644)

	_, err := c.PutFile("file_as_ns", "key", []byte{1, 2, 3})
	if err == nil {
		t.Error("expected PutFile to fail when directory creation fails")
	}

	err = c.Put("file_as_ns", "key", TestData{}, time.Hour)
	if err == nil {
		t.Error("expected Put to fail when directory creation fails")
	}
}

func TestCache_PutUnmarshalableType(t *testing.T) {
	tempDir := t.TempDir()
	c := NewInDir(tempDir, true)

	ch := make(chan int)
	err := c.Put("ns", "key", ch, time.Hour)
	if err == nil {
		t.Error("expected error when putting unmarshalable type")
	}
}

func TestCache_Disabled(t *testing.T) {
	var nilCache *Cache
	if nilCache.Enabled() {
		t.Error("nilCache.Enabled() should return false")
	}
	if nilCache.Get("ns", "key", &TestData{}) {
		t.Error("nilCache.Get should return false")
	}
	if err := nilCache.Put("ns", "key", TestData{}, time.Hour); err != nil {
		t.Errorf("nilCache.Put returned error: %v", err)
	}
	if _, found := nilCache.GetFile("ns", "key", time.Hour); found {
		t.Error("nilCache.GetFile should return false")
	}
	if _, err := nilCache.PutFile("ns", "key", []byte{}); err != nil {
		t.Errorf("nilCache.PutFile returned error: %v", err)
	}

	c := NewInDir(t.TempDir(), false)

	if c.Enabled() {
		t.Fatal("expected cache to be disabled")
	}

	var data TestData
	if c.Get("ns", "key", &data) {
		t.Error("disabled cache should never return a hit")
	}

	if err := c.Put("ns", "key", TestData{Name: "test"}, time.Hour); err != nil {
		t.Errorf("disabled cache Put returned error: %v", err)
	}

	if _, found := c.GetFile("ns", "key", time.Hour); found {
		t.Error("disabled cache GetFile should never return found")
	}

	if _, err := c.PutFile("ns", "key", []byte{}); err != nil {
		t.Errorf("disabled cache PutFile returned error: %v", err)
	}
}

func TestCache_Delete(t *testing.T) {
	tempDir := t.TempDir()
	c := NewInDir(tempDir, true)

	key := "delete_me"
	namespace := "test_ns"
	data := TestData{Name: "data", Count: 10}

	if err := c.Put(namespace, key, data, time.Hour); err != nil {
		t.Fatalf("failed to Put item: %v", err)
	}

	var fetched TestData
	if !c.Get(namespace, key, &fetched) {
		t.Fatal("expected cache hit before delete")
	}

	if err := c.Delete(namespace, key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if c.Get(namespace, key, &fetched) {
		t.Error("expected cache miss after delete")
	}

	// Test disabled cache Delete
	disabledCache := NewInDir(tempDir, false)
	if err := disabledCache.Delete(namespace, key); err != nil {
		t.Errorf("disabled cache Delete returned error: %v", err)
	}
}

func TestNew(t *testing.T) {
	c, err := New(true)
	if err != nil {
		t.Fatalf("New(true) failed: %v", err)
	}
	if !c.Enabled() {
		t.Error("New(true) expected enabled cache")
	}

	disabled, err := New(false)
	if err != nil {
		t.Fatalf("New(false) failed: %v", err)
	}
	if disabled.Enabled() {
		t.Error("New(false) expected disabled cache")
	}
}
