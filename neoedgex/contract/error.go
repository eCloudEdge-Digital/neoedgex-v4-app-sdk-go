package contract

type ErrorCode string

const (
	CodeInitializationError ErrorCode = "INITIALIZATION_ERROR"
	CodeNetworkError        ErrorCode = "NETWORK_ERROR"
	CodeProcessError        ErrorCode = "PROCESS_ERROR"
)
