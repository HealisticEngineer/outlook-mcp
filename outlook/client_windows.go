//go:build windows

package outlook

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

const (
	olFolderInbox = 6
	olMailItem    = 0
	maxResults    = 50

	MaxUnread          = 2000
	DefaultUnreadLimit = 5
)

// Client serializes all COM work onto one STA-bound OS thread.
type Client struct {
	reqChan chan func()
}

func NewClient() (*Client, error) {
	client := &Client{reqChan: make(chan func())}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
			ready <- fmt.Errorf("initialize COM apartment: %w", err)
			return
		}
		defer ole.CoUninitialize()
		ready <- nil
		for request := range client.reqChan {
			request()
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) dispatch(fn func() error) error {
	done := make(chan error, 1)
	c.reqChan <- func() { done <- fn() }
	return <-done
}

func (c *Client) ListUnread(params UnreadListParams) ([]EmailSummary, error) {
	if params.Limit < 1 || params.Limit > MaxUnread {
		return nil, fmt.Errorf("unread email limit must be between 1 and %d", MaxUnread)
	}
	if params.Offset < 0 || params.Offset >= MaxUnread {
		return nil, fmt.Errorf("unread email offset must be between 0 and %d", MaxUnread-1)
	}
	var emails []EmailSummary
	err := c.dispatch(func() error {
		items, err := inboxItems()
		if err != nil {
			return err
		}
		defer items.Release()
		filtered, err := oleutil.CallMethod(items, "Restrict", buildUnreadRestriction())
		if err != nil {
			return fmt.Errorf("restrict unread items: %w", err)
		}
		defer filtered.Clear()
		if _, err := oleutil.CallMethod(filtered.ToIDispatch(), "Sort", "[ReceivedTime]", true); err != nil {
			return fmt.Errorf("sort unread items: %w", err)
		}
		return collectEmailSummaries(filtered.ToIDispatch(), &emails, params.Offset, params.Limit, MaxUnread)
	})
	return emails, err
}

func (c *Client) ReadEmail(entryID string) (Email, error) {
	var email Email
	err := c.dispatch(func() error {
		item, err := itemFromID(entryID)
		if err != nil {
			return err
		}
		defer item.Release()
		email, err = emailFromItem(item)
		return err
	})
	return email, err
}

// MarkAsRead marks an Outlook mail item as read and saves the change.
func (c *Client) MarkAsRead(entryID string) error {
	if strings.TrimSpace(entryID) == "" {
		return fmt.Errorf("email EntryID is required")
	}
	return c.dispatch(func() error {
		item, err := itemFromID(entryID)
		if err != nil {
			return err
		}
		defer item.Release()
		if _, err := oleutil.PutProperty(item, "UnRead", false); err != nil {
			return fmt.Errorf("mark item as read: %w", err)
		}
		if _, err := oleutil.CallMethod(item, "Save"); err != nil {
			return fmt.Errorf("save read status: %w", err)
		}
		return nil
	})
}

func (c *Client) CreateDraft(params DraftParams) (string, error) {
	if strings.TrimSpace(params.To) == "" {
		return "", fmt.Errorf("draft recipient is required")
	}
	var entryID string
	err := c.dispatch(func() error {
		app, err := outlookApplication()
		if err != nil {
			return err
		}
		defer app.Release()
		value, err := oleutil.CallMethod(app, "CreateItem", olMailItem)
		if err != nil {
			return fmt.Errorf("create mail item: %w", err)
		}
		defer value.Clear()
		item := value.ToIDispatch()
		defer item.Release()
		for _, field := range []struct{ name, value string }{
			{"To", params.To}, {"Subject", params.Subject}, {"Body", params.Body},
		} {
			if _, err := oleutil.PutProperty(item, field.name, field.value); err != nil {
				return fmt.Errorf("set draft %s: %w", field.name, err)
			}
		}
		if _, err := oleutil.CallMethod(item, "Save"); err != nil {
			return fmt.Errorf("save draft: %w", err)
		}
		entryID, err = stringProperty(item, "EntryID")
		return err
	})
	return entryID, err
}

func (c *Client) SearchEmails(params SearchParams) ([]EmailSummary, error) {
	var emails []EmailSummary
	err := c.dispatch(func() error {
		items, err := inboxItems()
		if err != nil {
			return err
		}
		defer items.Release()
		filtered, err := oleutil.CallMethod(items, "Restrict", buildDASLQuery(params))
		if err != nil {
			return fmt.Errorf("restrict search items: %w", err)
		}
		defer filtered.Clear()
		if _, err := oleutil.CallMethod(filtered.ToIDispatch(), "Sort", "[ReceivedTime]", true); err != nil {
			return fmt.Errorf("sort search items: %w", err)
		}
		return collectEmailSummaries(filtered.ToIDispatch(), &emails, 0, maxResults, maxResults)
	})
	return emails, err
}

func buildUnreadRestriction() string {
	return "[UnRead] = True"
}

func buildDASLQuery(params SearchParams) string {
	const (
		subject  = "http://schemas.microsoft.com/mapi/proptag/0x0037001F"
		sender   = "http://schemas.microsoft.com/mapi/proptag/0x0C1F001F"
		received = "http://schemas.microsoft.com/mapi/proptag/0x0E060040"
	)
	conditions := make([]string, 0, 3)
	if query := strings.TrimSpace(params.Query); query != "" {
		conditions = append(conditions, fmt.Sprintf(`"%s" LIKE '%%%s%%'`, subject, escapeDASL(query)))
	}
	if senderValue := strings.TrimSpace(params.Sender); senderValue != "" {
		conditions = append(conditions, fmt.Sprintf(`"%s" LIKE '%%%s%%'`, sender, escapeDASL(senderValue)))
	}
	if !params.StartDate.IsZero() {
		conditions = append(conditions, fmt.Sprintf(`"%s" >= '%s'`, received, params.StartDate.Format("2006-01-02 15:04:05")))
	}
	if len(conditions) == 0 {
		return "@SQL=True"
	}
	return "@SQL=" + strings.Join(conditions, " AND ")
}

func escapeDASL(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func outlookApplication() (*ole.IDispatch, error) {
	unknown, err := oleutil.CreateObject("Outlook.Application")
	if err != nil {
		return nil, fmt.Errorf("create Outlook application: %w", err)
	}
	defer unknown.Release()
	app, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, fmt.Errorf("get Outlook dispatcher: %w", err)
	}
	return app, nil
}

func mapiNamespace() (*ole.IDispatch, error) {
	app, err := outlookApplication()
	if err != nil {
		return nil, err
	}
	defer app.Release()
	value, err := oleutil.CallMethod(app, "GetNamespace", "MAPI")
	if err != nil {
		return nil, fmt.Errorf("get MAPI namespace: %w", err)
	}
	defer value.Clear()
	namespace := value.ToIDispatch()
	namespace.AddRef()
	return namespace, nil
}

func inboxItems() (*ole.IDispatch, error) {
	namespace, err := mapiNamespace()
	if err != nil {
		return nil, err
	}
	defer namespace.Release()
	folder, err := oleutil.CallMethod(namespace, "GetDefaultFolder", olFolderInbox)
	if err != nil {
		return nil, fmt.Errorf("get Inbox: %w", err)
	}
	defer folder.Clear()
	value, err := oleutil.GetProperty(folder.ToIDispatch(), "Items")
	if err != nil {
		return nil, fmt.Errorf("get Inbox items: %w", err)
	}
	defer value.Clear()
	items := value.ToIDispatch()
	items.AddRef()
	return items, nil
}

func itemFromID(entryID string) (*ole.IDispatch, error) {
	namespace, err := mapiNamespace()
	if err != nil {
		return nil, err
	}
	defer namespace.Release()
	value, err := oleutil.CallMethod(namespace, "GetItemFromID", entryID)
	if err != nil {
		return nil, fmt.Errorf("get item %q: %w", entryID, err)
	}
	defer value.Clear()
	item := value.ToIDispatch()
	item.AddRef()
	return item, nil
}

func collectEmailSummaries(items *ole.IDispatch, emails *[]EmailSummary, offset, limit, maximum int) error {
	count, err := oleutil.GetProperty(items, "Count")
	if err != nil {
		return fmt.Errorf("get item count: %w", err)
	}
	defer count.Clear()
	countToRead := int(count.Val)
	if countToRead > maximum {
		countToRead = maximum
	}
	if offset >= countToRead {
		return nil
	}
	end := offset + limit
	if end > countToRead {
		end = countToRead
	}
	for index := offset + 1; index <= end; index++ {
		value, err := oleutil.CallMethod(items, "Item", index)
		if err != nil {
			return fmt.Errorf("get item %d: %w", index, err)
		}
		item := value.ToIDispatch()
		email, conversionErr := emailSummaryFromItem(item)
		value.Clear()
		if conversionErr != nil {
			return conversionErr
		}
		*emails = append(*emails, email)
	}
	return nil
}

func emailSummaryFromItem(item *ole.IDispatch) (EmailSummary, error) {
	var email EmailSummary
	var err error
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"EntryID", &email.ID}, {"Subject", &email.Subject}, {"SenderEmailAddress", &email.Sender},
	} {
		*field.target, err = stringProperty(item, field.name)
		if err != nil {
			return EmailSummary{}, err
		}
	}
	unread, err := oleutil.GetProperty(item, "UnRead")
	if err != nil {
		return EmailSummary{}, fmt.Errorf("get UnRead: %w", err)
	}
	email.IsUnread = unread.Val != 0
	unread.Clear()
	received, err := oleutil.GetProperty(item, "ReceivedTime")
	if err != nil {
		return EmailSummary{}, fmt.Errorf("get ReceivedTime: %w", err)
	}
	defer received.Clear()
	if value, ok := received.Value().(time.Time); ok {
		email.Received = value
	}
	return email, nil
}

func emailFromItem(item *ole.IDispatch) (Email, error) {
	var email Email
	var err error
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"EntryID", &email.ID}, {"Subject", &email.Subject}, {"SenderEmailAddress", &email.Sender}, {"Body", &email.Body},
	} {
		*field.target, err = stringProperty(item, field.name)
		if err != nil {
			return Email{}, err
		}
	}
	unread, err := oleutil.GetProperty(item, "UnRead")
	if err != nil {
		return Email{}, fmt.Errorf("get UnRead: %w", err)
	}
	email.IsUnread = unread.Val != 0
	unread.Clear()
	received, err := oleutil.GetProperty(item, "ReceivedTime")
	if err != nil {
		return Email{}, fmt.Errorf("get ReceivedTime: %w", err)
	}
	defer received.Clear()
	if value, ok := received.Value().(time.Time); ok {
		email.Received = value
	}
	return email, nil
}

func stringProperty(item *ole.IDispatch, name string) (string, error) {
	value, err := oleutil.GetProperty(item, name)
	if err != nil {
		return "", fmt.Errorf("get %s: %w", name, err)
	}
	defer value.Clear()
	return value.ToString(), nil
}
