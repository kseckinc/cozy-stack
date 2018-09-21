package vfs

import (
	"testing"
	"time"

	"github.com/cozy/cozy-stack/pkg/prefixer"
	"github.com/stretchr/testify/assert"
)

func TestDownloadStoreInMemory(t *testing.T) {
	downloadStoreTTL = 100 * time.Millisecond

	dbA := prefixer.NewPrefixer("alice.cozycloud.local", "alice.cozycloud.local")
	dbB := prefixer.NewPrefixer("bob.cozycloud.local", "bob.cozycloud.local")
	store := newMemStore()

	path := "/test/random/path.txt"
	slug := "drive"
	key1, err := store.AddFile(dbA, path, slug)
	assert.NoError(t, err)

	path2, slug2, err := store.GetFile(dbB, key1)
	assert.NoError(t, err)
	assert.Zero(t, path2, "Inter-instances store leaking")
	assert.Zero(t, slug2)

	path3, slug3, err := store.GetFile(dbA, key1)
	assert.NoError(t, err)
	assert.Equal(t, path, path3)
	assert.Equal(t, slug, slug3)

	time.Sleep(2 * downloadStoreTTL)

	path4, slug4, err := store.GetFile(dbA, key1)
	assert.NoError(t, err)
	assert.Zero(t, path4, "no expiration")
	assert.Zero(t, slug4)

	a := &Archive{
		Name: "test",
		Files: []string{
			"/archive/foo.jpg",
			"/archive/bar",
		},
	}
	key2, err := store.AddArchive(dbA, a)
	assert.NoError(t, err)

	a2, err := store.GetArchive(dbA, key2)
	assert.NoError(t, err)
	assert.Equal(t, a, a2)

	time.Sleep(2 * downloadStoreTTL)

	a3, err := store.GetArchive(dbA, key2)
	assert.NoError(t, err)
	assert.Nil(t, a3, "no expiration")
}

func TestDownloadStoreInRedis(t *testing.T) {
	downloadStoreTTL = 100 * time.Millisecond

	dbA := prefixer.NewPrefixer("alice.cozycloud.local", "alice.cozycloud.local")
	dbB := prefixer.NewPrefixer("bob.cozycloud.local", "bob.cozycloud.local")
	store := GetStore()

	path := "/test/random/path.txt"
	slug := "photos"
	key1, err := store.AddFile(dbA, path, slug)
	assert.NoError(t, err)

	path2, slug2, err := store.GetFile(dbB, key1)
	assert.NoError(t, err)
	assert.Zero(t, path2, "Inter-instances store leaking")
	assert.Zero(t, slug2)

	path3, slug3, err := store.GetFile(dbA, key1)
	assert.NoError(t, err)
	assert.Equal(t, path, path3)
	assert.Equal(t, slug, slug3)

	time.Sleep(2 * downloadStoreTTL)

	path4, slug4, err := store.GetFile(dbA, key1)
	assert.NoError(t, err)
	assert.Zero(t, path4, "no expiration")
	assert.Zero(t, slug4)

	a := &Archive{
		Name: "test",
		Files: []string{
			"/archive/foo.jpg",
			"/archive/bar",
		},
	}
	key2, err := store.AddArchive(dbA, a)
	assert.NoError(t, err)

	a2, err := store.GetArchive(dbA, key2)
	assert.NoError(t, err)
	assert.Equal(t, a, a2)

	time.Sleep(2 * downloadStoreTTL)

	a3, err := store.GetArchive(dbA, key2)
	assert.NoError(t, err)
	assert.Nil(t, a3, "no expiration")
}
