package tui

import (
	"testing"

	"github.com/yueleshia/streamsurf/src"
	a "github.com/yueleshia/streamsurf/src/testify"
)


//run: go test

func lru(size int) LRU {
	return LRU {
		Buffer: make([]src.Video, size),
		Exists: make(map[string]int, size * 2),
	}
}

func TestAdd(t *testing.T) {
	var cache = lru(10)
	cache.Push(src.Video { Url: "a" })
	cache.Push(src.Video { Url: "b" })
	a.AssertEqual(t, src.Video{ Url: "a" }, cache.Buffer[0])
	a.AssertEqual(t, src.Video{ Url: "b" }, cache.Buffer[1])
	a.AssertEqual(t, src.Video{}, cache.Buffer[2])
}

func TestDuplicate(t *testing.T) {
	var cache = lru(10)
	cache.Push(src.Video { Url: "a" })
	cache.Push(src.Video { Url: "b" })
	cache.Push(src.Video { Url: "b" })
	a.AssertEqual(t, src.Video{ Url: "a" }, cache.Buffer[0])
	a.AssertEqual(t, src.Video{}, cache.Buffer[1])
	a.AssertEqual(t, src.Video{ Url: "b" }, cache.Buffer[2])
}

func TestWrap(t *testing.T) {
	var cache = lru(3)
	cache.Push(src.Video { Url: "a" })
	cache.Push(src.Video { Url: "b" })
	cache.Push(src.Video { Url: "c" })
	cache.Push(src.Video { Url: "d" })
	cache.Push(src.Video { Url: "c" })
	a.AssertEqual(t, src.Video{ Url: "d" }, cache.Buffer[0])
	a.AssertEqual(t, src.Video{ Url: "c" }, cache.Buffer[1])
	a.AssertEqual(t, src.Video{}, cache.Buffer[2])
}

