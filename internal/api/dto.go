package api

// sendPushRequest is the body of POST /v1/push/send.
type sendPushRequest struct {
	Platform string `json:"platform"`
	Token    string `json:"token"`
}
