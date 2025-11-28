package response

import (
	"net/http"
	"testing"
)

func TestGetStatusText(t *testing.T) {
	t.Log(GetStatusText(http.StatusInsufficientStorage))
}

func TestGetStatusLetterText(t *testing.T) {
	t.Log(GetStatusLowerText(http.StatusExpectationFailed))
}
