package outlook

import "time"

type Email struct {
	ID       string    `json:"id"`
	Subject  string    `json:"subject"`
	Sender   string    `json:"sender"`
	Received time.Time `json:"received"`
	Body     string    `json:"body"`
	IsUnread bool      `json:"is_unread"`
}

type SearchParams struct {
	Query     string    `json:"query"`
	Sender    string    `json:"sender"`
	StartDate time.Time `json:"start_date"`
}

type DraftParams struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type ReplyParams struct {
	EmailID string `json:"email_id"`
	Body    string `json:"body"`
}
