package graphjson

import (
	"encoding/json"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

func Render(g view.Graph) ([]byte, error) {
	return json.MarshalIndent(g, "", "  ")
}
