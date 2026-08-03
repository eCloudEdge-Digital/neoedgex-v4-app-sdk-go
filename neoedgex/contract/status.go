package contract

// Error is the payload NodeEnv.ReportError publishes on the node's error topic.
type Error struct {
	// Code is the string form of an ErrorCode.
	Code string `json:"code"`
	// Detail is the reported error's message, empty when no error was given.
	Detail string `json:"detail"`
	// UpdatedAt is when the error was reported, in Unix seconds.
	UpdatedAt int64 `json:"updatedAt"`
}
