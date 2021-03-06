// +build windows

package eventlog

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	elog "github.com/andrewkroh/sys/windows/svc/eventlog"
	"github.com/elastic/libbeat/logp"
	"github.com/joeshaw/multierror"
	"github.com/stretchr/testify/assert"
)

// Names that are registered by the test for logging events.
const (
	providerName = "Winlogbeat"
	sourceName   = "Integration Test"
)

// Event message files used when logging events.
const (
	// EventCreate.exe has valid event IDs in the range of 1-1000 where each
	// event message requires a single parameter.
	eventCreateMsgFile = "%SystemRoot%\\System32\\EventCreate.exe"
	// services.exe is used by the Service Control Manager as its event message
	// file; these tests use it to log messages with more than one parameter.
	servicesMsgFile = "%SystemRoot%\\System32\\services.exe"
)

const allLevels = elog.Success | elog.AuditFailure | elog.AuditSuccess | elog.Error | elog.Info | elog.Warning

// Test messages.
var messages = map[uint32]struct {
	eventType uint16
	message   string
}{
	1: {
		eventType: elog.Info,
		message:   "Hmmmm.",
	},
	2: {
		eventType: elog.Success,
		message:   "I am so blue I'm greener than purple.",
	},
	3: {
		eventType: elog.Warning,
		message:   "I stepped on a Corn Flake, now I'm a Cereal Killer.",
	},
	4: {
		eventType: elog.Error,
		message:   "The quick brown fox jumps over the lazy dog.",
	},
	5: {
		eventType: elog.AuditSuccess,
		message:   "Where do random thoughts come from?",
	},
	6: {
		eventType: elog.AuditFailure,
		message:   "Login failure for user xyz!",
	},
}

var oneTimeLogpInit sync.Once

// Initializes logp if the verbose flag was set.
func configureLogp() {
	oneTimeLogpInit.Do(func() {
		if testing.Verbose() {
			logp.LogInit(logp.LOG_DEBUG, "", false, true, []string{"eventlog"})
			logp.Info("DEBUG enabled for eventlog.")
		} else {
			logp.LogInit(logp.LOG_WARNING, "", false, true, []string{})
		}
	})
}

// initLog initializes an event logger. It registers the source name with
// the registry if it does not already exist.
func initLog(provider, source, msgFile string) (*elog.Log, error) {
	// Install entry to registry:
	_, err := elog.Install(providerName, sourceName, msgFile, true, allLevels)
	if err != nil {
		return nil, err
	}

	// Open a new logger for writing events:
	log, err := elog.Open(sourceName)
	if err != nil {
		var errs multierror.Errors
		errs = append(errs, err)
		err := elog.RemoveSource(providerName, sourceName)
		if err != nil {
			errs = append(errs, err)
		}
		err = elog.RemoveProvider(providerName)
		if err != nil {
			errs = append(errs, err)
		}
		return nil, errs.Err()
	}

	return log, nil
}

// uninstallLog unregisters the event logger from the registry and closes the
// log's handle if it is open.
func uninstallLog(provider, source string, log *elog.Log) error {
	var errs multierror.Errors

	if log != nil {
		err := clearEventLog(Handle(log.Handle), "")
		if err != nil {
			errs = append(errs, err)
		}

		err = log.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}

	err := elog.RemoveSource(providerName, sourceName)
	if err != nil {
		errs = append(errs, err)
	}

	err = elog.RemoveProvider(providerName)
	if err != nil {
		errs = append(errs, err)
	}

	return errs.Err()
}

// Verify that all messages are read from the event log.
func TestRead(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode.")
	}
	configureLogp()
	log, err := initLog(providerName, sourceName, eventCreateMsgFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := uninstallLog(providerName, sourceName, log)
		if err != nil {
			t.Fatal(err)
		}
	}()

	// Publish test messages:
	for k, m := range messages {
		err = log.Report(m.eventType, k, []string{m.message})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Read messages:
	eventlog := NewEventLoggingAPI(providerName)
	err = eventlog.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := eventlog.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	records, err := eventlog.Read()
	if err != nil {
		t.Fatal(err)
	}

	// Validate messages:
	assert.Len(t, records, len(messages))
	for _, record := range records {
		t.Log(record)
		m, exists := messages[record.EventID]
		if !exists {
			t.Errorf("Unknown EventId %d Read() from event log. %v", record.EventID, record)
			continue
		}
		assert.Equal(t, m.eventType, uint16(record.EventType))
		assert.Equal(t, m.message, strings.TrimRight(record.Message, "\r\n"))
	}

	// Validate getNumberOfEventLogRecords returns the correct number of messages.
	numMessages, err := getNumberOfEventLogRecords(Handle(log.Handle))
	assert.NoError(t, err)
	assert.Equal(t, len(messages), int(numMessages))
}

// Test that when an unknown Event ID is found, that a message containing the
// insert strings (the message parameters) is returned.
func TestReadUnknownEventId(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode.")
	}
	configureLogp()
	log, err := initLog(providerName, sourceName, servicesMsgFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := uninstallLog(providerName, sourceName, log)
		if err != nil {
			t.Fatal(err)
		}
	}()

	var eventID uint32 = 1000
	msg := "Test Message"
	err = log.Success(eventID, msg)
	if err != nil {
		t.Fatal(err)
	}

	// Read messages:
	eventlog := NewEventLoggingAPI(providerName)
	err = eventlog.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := eventlog.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	records, err := eventlog.Read()
	if err != nil {
		t.Fatal(err)
	}

	// Verify the error message:
	assert.Len(t, records, 1)
	assert.Equal(t, eventID, records[0].EventID)
	assert.Equal(t, fmt.Sprintf(noMessageFile, eventID, sourceName, msg),
		strings.TrimRight(records[0].Message, "\r\n"))
}

// Test that multiple event message files are searched for an event ID. This
// test configures the "EventMessageFile" registry value as a semi-color
// separated list of files. If the message for an event ID is not found in one
// of the files then the next file should be checked.
func TestReadTriesMultipleEventMsgFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode.")
	}
	configureLogp()
	log, err := initLog(providerName, sourceName,
		servicesMsgFile+";"+eventCreateMsgFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := uninstallLog(providerName, sourceName, log)
		if err != nil {
			t.Fatal(err)
		}
	}()

	var eventID uint32 = 1000
	msg := "Test Message"
	err = log.Success(eventID, msg)
	if err != nil {
		t.Fatal(err)
	}

	// Read messages:
	eventlog := NewEventLoggingAPI(providerName)
	err = eventlog.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := eventlog.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	records, err := eventlog.Read()
	if err != nil {
		t.Fatal(err)
	}

	// Verify the error message:
	assert.Len(t, records, 1)
	assert.Equal(t, eventID, records[0].EventID)
	assert.Equal(t, msg, strings.TrimRight(records[0].Message, "\r\n"))
}

// Test event messages that require more than one message parameter.
func TestReadMultiParameterMsg(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode.")
	}
	configureLogp()
	log, err := initLog(providerName, sourceName, servicesMsgFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := uninstallLog(providerName, sourceName, log)
		if err != nil {
			t.Fatal(err)
		}
	}()

	// EventID observed by exporting system event log to XML and doing calculation.
	// <EventID Qualifiers="16384">7036</EventID>
	// 1073748860 = 16384 << 16 + 7036
	// https://msdn.microsoft.com/en-us/library/windows/desktop/aa385206(v=vs.85).aspx
	var eventID uint32 = 1073748860
	template := "The %s service entered the %s state."
	msgs := []string{"Windows Update", "running"}
	err = log.Report(elog.Info, eventID, msgs)
	if err != nil {
		t.Fatal(err)
	}

	// Read messages:
	eventlog := NewEventLoggingAPI(providerName)
	err = eventlog.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := eventlog.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	records, err := eventlog.Read()
	if err != nil {
		t.Fatal(err)
	}

	// Verify the message contents:
	assert.Len(t, records, 1)
	assert.Equal(t, eventID, records[0].EventID)
	assert.Equal(t, fmt.Sprintf(template, msgs[0], msgs[1]),
		strings.TrimRight(records[0].Message, "\r\n"))
}

// Verify that opening an invalid provider succeeds. Windows opens the
// Application event log provider when this happens (unfortunately).
func TestOpenInvalidProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode.")
	}
	configureLogp()

	el := NewEventLoggingAPI("nonExistentProvider")
	assert.NoError(t, el.Open(0), "Calling Open() on an unknown provider "+
		"should automatically open Application.")
	_, err := el.Read()
	assert.NoError(t, err)
}

// TODO: Add more test cases:
// - Log rotation (clear log while reading)
// - Record number rollover (there may not be an issue with this)
// - Message that requires no string inserts.
// - Reading from a source name instead of provider name.
// - Resume reading from a given record number.
