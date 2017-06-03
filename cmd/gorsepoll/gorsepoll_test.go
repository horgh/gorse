package main

import (
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/horgh/rss"
)

// The feed has not been polled.
func TestHasBeenPolledNotPolled(t *testing.T) {
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
		`SELECT 1 FROM rss_item WHERE rss_feed_id = \$1`).
		WillReturnRows(rows0)

	rows1 := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery(
		`SELECT 1 FROM rss_item_archive WHERE rss_feed_id = \$1`).
		WillReturnRows(rows1)

	mock.ExpectClose()

	polled, err := hasFeedBeenPolled(db, 1)
	if err != nil {
		t.Fatalf("checking polled error: %s", err)
	}

	want := false
	if polled != want {
		t.Errorf("polled = %#v, wanted %#v", polled, want)
	}
}

// The feed has been polled - items in main table.
func TestHasBeenPolledYesPolled(t *testing.T) {
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
		`SELECT 1 FROM rss_item WHERE rss_feed_id = \$1`).
		WillReturnRows(rows0)

	mock.ExpectClose()

	polled, err := hasFeedBeenPolled(db, 1)
	if err != nil {
		t.Fatalf("checking polled error: %s", err)
	}

	want := true
	if polled != want {
		t.Errorf("polled = %#v, wanted %#v", polled, want)
	}
}

// The feed has been polled - items in archive table.
func TestHasBeenPolledYesPolledInArchive(t *testing.T) {
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
		`SELECT 1 FROM rss_item WHERE rss_feed_id = \$1`).
		WillReturnRows(rows0)

	rows1 := sqlmock.NewRows([]string{"id"})
	rows1.AddRow(1)
	mock.ExpectQuery(
		`SELECT 1 FROM rss_item_archive WHERE rss_feed_id = \$1`).
		WillReturnRows(rows1)

	mock.ExpectClose()

	polled, err := hasFeedBeenPolled(db, 1)
	if err != nil {
		t.Fatalf("checking polled error: %s", err)
	}

	want := true
	if polled != want {
		t.Errorf("polled = %#v, wanted %#v", polled, want)
	}
}

// Test where the item exists in the database by its GUID.
func TestShouldRecordItemExistsByGUID(t *testing.T) {
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
	feed := &DBFeed{HasBeenPolled: true}
	item := &rss.Item{
		GUID: "test-guid",
	}
	cutoffTime := time.Now()
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

// Test where the item exists in the database by its GUID in archive table.
func TestShouldRecordItemExistsByGUIDInArchiveTable(t *testing.T) {
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
		`SELECT id FROM rss_item_archive WHERE rss_feed_id = \$1 AND guid = \$2`).
		WillReturnRows(rows1)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	feed := &DBFeed{HasBeenPolled: true}
	item := &rss.Item{
		GUID: "test-guid",
	}
	cutoffTime := time.Now()
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

// Item has GUID and it isn't recorded.
//
// Cut off time indicates it shouldn't be recorded but we only look at GUID.
func TestShouldRecordItemHasGUIDButDoesNotExist(t *testing.T) {
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
		`SELECT id FROM rss_item_archive WHERE rss_feed_id = \$1 AND guid = \$2`).
		WillReturnRows(rows1)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	feed := &DBFeed{HasBeenPolled: true}
	cutoffTime := time.Now()
	item := &rss.Item{
		GUID:    "test-guid",
		PubDate: cutoffTime.Add(-time.Duration(10) * time.Hour),
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

// Test where an item has no GUID and the item's link is in the main table.
func TestShouldRecordItemExistsByLink(t *testing.T) {
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
		`SELECT id FROM rss_item WHERE rss_feed_id = \$1 AND link = \$2`).
		WillReturnRows(rows0)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	feed := &DBFeed{HasBeenPolled: true}
	item := &rss.Item{}
	cutoffTime := time.Now()
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

// Test where an item has no GUID and the item's link is in the archive table.
func TestShouldRecordItemExistsByLinkInArchiveTable(t *testing.T) {
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

	rows1 := sqlmock.NewRows([]string{"id"})
	rows1.AddRow(1)
	mock.ExpectQuery(
		`SELECT id FROM rss_item_archive WHERE rss_feed_id = \$1 AND link = \$2`).
		WillReturnRows(rows1)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	feed := &DBFeed{HasBeenPolled: true}
	item := &rss.Item{}
	cutoffTime := time.Now()
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

// Test where the item has no GUID and the item's link is not in the database.
//
// We don't want to record it because of publication time
func TestShouldRecordItemDoesNotExistByLinkAndPubDateSaysNo(t *testing.T) {
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

	rows1 := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery(
		`SELECT id FROM rss_item_archive WHERE rss_feed_id = \$1 AND link = \$2`).
		WillReturnRows(rows1)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	feed := &DBFeed{HasBeenPolled: true}
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

// Test where the item has no GUID and the item's link is not in the database.
//
// Force wanting to record it by ignoring publication times.
func TestShouldRecordItemDoesNotExistByLinkAndForceRecord(t *testing.T) {
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

	rows1 := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery(
		`SELECT id FROM rss_item_archive WHERE rss_feed_id = \$1 AND link = \$2`).
		WillReturnRows(rows1)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	feed := &DBFeed{HasBeenPolled: true}
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

// Test where the item has no GUID and the item's link is not in the database.
//
// We want to record it by due to publication time.
func TestShouldRecordItemDoesNotExistByLinkAndWantToRecord(t *testing.T) {
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

	rows1 := sqlmock.NewRows([]string{"id"})
	mock.ExpectQuery(
		`SELECT id FROM rss_item_archive WHERE rss_feed_id = \$1 AND link = \$2`).
		WillReturnRows(rows1)

	mock.ExpectClose()

	config := &Config{Quiet: 1}
	feed := &DBFeed{HasBeenPolled: true}
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
