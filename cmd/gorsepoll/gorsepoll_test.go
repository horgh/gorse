package main

import (
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/horgh/rss"
)

// Item does not exist. No GUID. Publication date is too old. No record.
func TestShouldRecordItem0(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("unable to open mock db: %s", err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing db failed: %s", err)
		}
	}()

	rows0 := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery(
		`SELECT id FROM rss_item WHERE rss_feed_id = \$1 AND link = \$2`).
		WillReturnRows(rows0)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	lastUpdateTime := time.Now()
	feed := &DBFeed{LastUpdateTime: &lastUpdateTime}
	cutoffTime := time.Now()
	item := &rss.Item{
		PubDate: cutoffTime.Add(-time.Duration(10) * time.Hour),
	}
	ignorePublicationTimes := false

	record, err := shouldRecordItem(config, db, feed, item, cutoffTime,
		ignorePublicationTimes)
	if err != nil {
		t.Fatalf("checking whether to record raised error: %s", err)
	}

	want := false
	if record != want {
		t.Errorf("record = %#v, wanted %#v", record, want)
	}
}

// Item does not exist. No GUID. Publication date is too old. Force record.
func TestShouldRecordItem1(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("unable to open mock db: %s", err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing db failed: %s", err)
		}
	}()

	rows0 := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery(
		`SELECT id FROM rss_item WHERE rss_feed_id = \$1 AND link = \$2`).
		WillReturnRows(rows0)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	lastUpdateTime := time.Now()
	feed := &DBFeed{LastUpdateTime: &lastUpdateTime}
	cutoffTime := time.Now()
	item := &rss.Item{
		PubDate: cutoffTime.Add(-time.Duration(10) * time.Hour),
	}
	ignorePublicationTimes := true

	record, err := shouldRecordItem(config, db, feed, item, cutoffTime,
		ignorePublicationTimes)
	if err != nil {
		t.Fatalf("checking whether to record raised error: %s", err)
	}

	want := true
	if record != want {
		t.Errorf("record = %#v, wanted %#v", record, want)
	}
}

// Item does not exist. No GUID. Publication date is okay. Record.
func TestShouldRecordItem2(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("unable to open mock db: %s", err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing db failed: %s", err)
		}
	}()

	rows0 := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery(
		`SELECT id FROM rss_item WHERE rss_feed_id = \$1 AND link = \$2`).
		WillReturnRows(rows0)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	lastUpdateTime := time.Now()
	feed := &DBFeed{LastUpdateTime: &lastUpdateTime}
	cutoffTime := time.Now()
	item := &rss.Item{
		PubDate: cutoffTime.Add(time.Duration(10) * time.Hour),
	}
	ignorePublicationTimes := false

	record, err := shouldRecordItem(config, db, feed, item, cutoffTime,
		ignorePublicationTimes)
	if err != nil {
		t.Fatalf("checking whether to record raised error: %s", err)
	}

	want := true
	if record != want {
		t.Errorf("record = %#v, wanted %#v", record, want)
	}
}

// Item does not exist. GUID. Record.
func TestShouldRecordItem3(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("unable to open mock db: %s", err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing db failed: %s", err)
		}
	}()

	rows0 := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery(
		`SELECT id FROM rss_item WHERE rss_feed_id = \$1 AND guid = \$2`).
		WillReturnRows(rows0)

	rows1 := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery(
		`SELECT id FROM rss_item WHERE rss_feed_id = \$1 AND link = \$2`).
		WillReturnRows(rows1)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	lastUpdateTime := time.Now()
	feed := &DBFeed{LastUpdateTime: &lastUpdateTime}
	cutoffTime := time.Now()
	item := &rss.Item{
		GUID:    "test-guid",
		PubDate: cutoffTime.Add(time.Duration(10) * time.Hour),
	}
	ignorePublicationTimes := false

	record, err := shouldRecordItem(config, db, feed, item, cutoffTime,
		ignorePublicationTimes)
	if err != nil {
		t.Fatalf("checking whether to record raised error: %s", err)
	}

	want := true
	if record != want {
		t.Errorf("record = %#v, wanted %#v", record, want)
	}
}

// Item exists by GUID. No record.
func TestShouldRecordItem4(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("unable to open mock db: %s", err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing db failed: %s", err)
		}
	}()

	rows0 := sqlmock.NewRows([]string{"id"})
	rows0.AddRow(1)
	mock.ExpectQuery(
		`SELECT id FROM rss_item WHERE rss_feed_id = \$1 AND guid = \$2`).
		WillReturnRows(rows0)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	lastUpdateTime := time.Now()
	feed := &DBFeed{LastUpdateTime: &lastUpdateTime}
	cutoffTime := time.Now()
	item := &rss.Item{
		GUID:    "test-guid",
		PubDate: cutoffTime.Add(time.Duration(10) * time.Hour),
	}
	ignorePublicationTimes := false

	record, err := shouldRecordItem(config, db, feed, item, cutoffTime,
		ignorePublicationTimes)
	if err != nil {
		t.Fatalf("checking whether to record raised error: %s", err)
	}

	want := false
	if record != want {
		t.Errorf("record = %#v, wanted %#v", record, want)
	}
}

// Item exists by link. No record.
func TestShouldRecordItem5(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("unable to open mock db: %s", err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing db failed: %s", err)
		}
	}()

	rows0 := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery(
		`SELECT id FROM rss_item WHERE rss_feed_id = \$1 AND guid = \$2`).
		WillReturnRows(rows0)

	rows1 := sqlmock.NewRows([]string{"id"})
	rows1.AddRow(1)
	mock.ExpectQuery(
		`SELECT id FROM rss_item WHERE rss_feed_id = \$1 AND link = \$2`).
		WillReturnRows(rows1)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	lastUpdateTime := time.Now()
	feed := &DBFeed{LastUpdateTime: &lastUpdateTime}
	cutoffTime := time.Now()
	item := &rss.Item{
		GUID:    "test-guid",
		PubDate: cutoffTime.Add(time.Duration(10) * time.Hour),
	}
	ignorePublicationTimes := false

	record, err := shouldRecordItem(config, db, feed, item, cutoffTime,
		ignorePublicationTimes)
	if err != nil {
		t.Fatalf("checking whether to record raised error: %s", err)
	}

	want := false
	if record != want {
		t.Errorf("record = %#v, wanted %#v", record, want)
	}
}
