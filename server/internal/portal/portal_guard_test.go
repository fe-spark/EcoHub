package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestProvideHandler_RejectsExcessiveIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parts := make([]string, 105)
	for i := range parts {
		parts[i] = "1"
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/provide/vod?ac=detail&ids="+strings.Join(parts, ","), nil)
	ProvideHd.HandleProvide(c)
	if !strings.Contains(w.Body.String(), "不能超过 100") {
		t.Fatalf("expected excessive ids rejection, got %s", w.Body.String())
	}
}
