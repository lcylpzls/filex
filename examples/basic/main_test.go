package main

import (
	"testing"

	"github.com/lcylpzls/testx"
)

func TestRun(t *testing.T) {
	testx.RequireNoError(t, run(t.TempDir()))
}
