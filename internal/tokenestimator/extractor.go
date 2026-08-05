package tokenestimator

import "encoding/json"

func ExtractModel(bodyBytes []byte) string {
	if len(bodyBytes) == 0 {
		return ""
	}
	var reqBody struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		return ""
	}
	return reqBody.Model
}
