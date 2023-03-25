package tests

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindRoot(t *testing.T) {
	root, err := FindRoot()
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(root, "lotus"))
}
