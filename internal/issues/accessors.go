package issues

import "time"

// Public accessors over the committed creation for the web layer; the fields
// themselves stay private so every write path funnels through the service.
func (r CreationReceipt) IssueID() string        { return r.Result.issueID }
func (r CreationReceipt) IssueNumber() int64     { return r.Result.issueNumber }
func (r CreationReceipt) ChangeSetID() string    { return r.Result.mainChangeSetID }
func (r CreationReceipt) ConversationID() string { return r.Result.conversationID }
func (r CreationReceipt) ConfigurationRevision() string {
	return string(r.Result.configurationRevision)
}
func (r CreationReceipt) CreatedAt() time.Time { return r.Result.createdAt }
func (c CreationOptions) Cursor() string       { return c.NextCursor }
