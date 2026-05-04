package contract

type Error struct {
	Code      string `json:"code"`
	Detail    string `json:"detail"`
	UpdatedAt int64  `json:"updatedAt"`
}
