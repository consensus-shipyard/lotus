package fs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindRoot(t *testing.T) {
	_, err := FindRoot()
	require.NoError(t, err)
}
