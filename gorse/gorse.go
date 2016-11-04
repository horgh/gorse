//
// gorse is a web front end to a database of RSS feeds/items. The database gets
// populated by my RSS poller, gorsepoll.
//
// The interface shows items from feeds and allows flagging them as read.
//
// For the database schema, refer to gorsepoll.
//
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/fcgi"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/context"
	"github.com/gorilla/sessions"
	_ "github.com/lib/pq"
	"summercat.com/config"
	"summercat.com/gorse/gorselib"
)

// GorseConfig holds runtime configuration information.
type GorseConfig struct {
	ListenHost string
	ListenPort uint64
	DBUser     string
	DBPass     string
	DBName     string
	DBHost     string

	// TODO: Auto detect timezone, or move this to a user setting
	DisplayTimeZone string

	URIPrefix               string
	CookieAuthenticationKey string
	SessionName             string
}

// DB is the connection to the database.
// This is so we try to share a single connection for multiple requests.
// NOTE: According to the database/sql documentation, the DB type
//   is indeed safe for concurrent use by multiple goroutines.
var DB *sql.DB

// DBLock helps us avoid race conditions associated with the database. Such as
// connecting to it (assigning the global).
var DBLock sync.Mutex

// HTTPHandler holds functions/data used to service HTTP requests.
// We need this struct as we must pass instances of it to fcgi.Serve.
// This is because it must conform to the http.Handler interface.
type HTTPHandler struct {
	settings     *GorseConfig
	sessionStore *sessions.CookieStore
}

type sortOrder int

const (
	sortAscending sortOrder = iota
	sortDescending
)

// ReadState holds an item's state (rss_item_state table, read_state type).
type ReadState int

const (
	// Unread means the item is not yet read.
	Unread ReadState = iota
	// Read means the item was read.
	Read
	// ReadLater means to save the item to read later.
	ReadLater
)

const pageSize = 50

// send400Error sends a bad request error with the given message in the body.
func send400Error(rw http.ResponseWriter, message string) {
	rw.WriteHeader(http.StatusBadRequest)
	_, _ = rw.Write([]byte("<h1>" + template.HTMLEscapeString(message) + "</h1>"))
}

// send500Error sends an internal server error with the given message in the
// body.
func send500Error(rw http.ResponseWriter, message string) {
	rw.WriteHeader(http.StatusInternalServerError)
	_, _ = rw.Write([]byte("<h1>" + template.HTMLEscapeString(message) + "</h1>"))
}

// getRowCSSClass takes a row index and determines the css class to use.
func getRowCSSClass(index int) string {
	if index%2 == 0 {
		return "row1"
	}
	return "row2"
}

// getHTMLDescription builds the HTML encoded description.
// we call it while generating HTML.
// text is the unencoded string, and we return HTML encoded.
// we have this so we can make inline urls into links.
func getHTMLDescription(text string) template.HTML {
	// encode the entire string as HTML first.
	html := template.HTMLEscapeString(text)

	// wrap up URLs in <a>.
	// I previously used this re: \b(https?://\S+)
	// but there were issues with it recognising non-url characters. I even
	// found it match a space which seems like it should be impossible.
	re := regexp.MustCompile(`\b(https?://[A-Za-z0-9\-\._~:/\?#\[\]@!\$&'\(\)\*\+,;=]+)`)
	return template.HTML(re.ReplaceAllString(html, `<a href="$1">$1</a>`))
}

// renderPage builds a full page. the specified content template is
// used to build the content section of the page wrapped between
// header and footer.
func renderPage(rw http.ResponseWriter, contentTemplate string,
	data interface{}) error {
	// ensure the specified content template is valid.
	matched, err := regexp.MatchString("^[_a-zA-Z]+$", contentTemplate)
	if err != nil || !matched {
		return errors.New("Invalid template name")
	}

	header, err := template.ParseFiles("templates/_header.html")
	if err != nil {
		log.Printf("Failed to load header: %s", err)
		return err
	}

	// content.
	funcMap := template.FuncMap{
		"getRowCSSClass": getRowCSSClass,
	}
	// we need the base path as that is the name that gets assigned
	// to the template internally due to how we create the template.
	// that is, through New(), then ParseFiles() - ParseFiles() sets
	// the name of the template using the basename of the file.
	contentTemplateBasePath := contentTemplate + ".html"
	contentTemplatePath := "templates/" + contentTemplateBasePath
	content, err := template.New("content").Funcs(funcMap).ParseFiles(contentTemplatePath)
	if err != nil {
		log.Printf("Failed to load content template [%s]: %s", contentTemplate, err)
		return err
	}

	// footer.
	footer, err := template.ParseFiles("templates/_footer.html")
	if err != nil {
		log.Printf("Failed to load footer: %s", err)
		return err
	}

	// execute the templates and write them out.
	err = header.Execute(rw, data)
	if err != nil {
		log.Printf("Failed to execute header: %s", err)
		return err
	}
	err = content.ExecuteTemplate(rw, contentTemplateBasePath, data)
	if err != nil {
		log.Printf("Failed to execute content: %s", err)
		return err
	}
	err = footer.Execute(rw, data)
	if err != nil {
		log.Printf("Failed to execute footer: %s", err)
		return err
	}
	return nil
}

// handlerListItems handles a list rss items request and builds an html
// response.
// it implements the type RequestHandlerFunc
func handlerListItems(rw http.ResponseWriter, request *http.Request,
	settings *GorseConfig, session *sessions.Session) {

	db, err := getDB(settings)
	if err != nil {
		log.Printf("Failed to get database connection: %s", err)
		send500Error(rw, "Failed to connect to database")
		return
	}

	// Retrieve the feeds from the database. we want to be able to
	// list our feeds and show information such as when the last time
	// we updated was.
	feeds, err := gorselib.RetrieveFeeds(db)
	if err != nil {
		log.Printf("Failed to retrieve feeds: %s", err)
		send500Error(rw, "Failed to retrieve feeds")
		return
	}

	// We can be told different sort display order. This is in the URL.
	requestValues := request.URL.Query()

	// Default is date descending.
	order := sortDescending
	reverseSortOrder := "date-asc"

	sortRaw := requestValues.Get("sort-order")
	if sortRaw == "" || sortRaw == "date-desc" {
		order = sortDescending
		reverseSortOrder = "date-asc"
	}
	if sortRaw == "date-asc" {
		order = sortAscending
		reverseSortOrder = "date-desc"
	}

	page := 1
	pageParam := requestValues.Get("page")
	if pageParam != "" {
		page, err = strconv.Atoi(pageParam)
		if err != nil {
			page = 1
		}
	}

	userIDStr := requestValues.Get("user-id")
	if userIDStr == "" {
		log.Printf("No user ID found")
		send400Error(rw, "No user ID found.")
		return
	}
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		log.Printf("Invalid user ID: %s: %s", userIDStr, err)
		send500Error(rw, "Invalid user ID.")
		return
	}

	// We either view unread or read later items. Those marked read we never can
	// see again currently.
	readState := Unread
	requestedReadState := requestValues.Get("read-state")
	if requestedReadState == "read-later" {
		readState = ReadLater
	}

	// Retrieve items from the database.
	items, err := dbRetrieveFeedItems(db, settings, order, page, userID, readState)
	if err != nil {
		log.Printf("Failed to retrieve items: %s", err)
		send500Error(rw, "Failed to retrieve items")
		return
	}

	// Set up additional information about each item.
	// Specifically we want to set a string timestamp.
	for i, item := range items {
		// format time.
		items[i].PublicationDateString = item.PublicationDate.Format(time.RFC1123Z)

		// ensure we say no title if there is no title.
		// (so there is something to have in the link content)
		if len(items[i].Title) == 0 {
			items[i].Title = "<No title>"
		}

		// make HTML version of description. we set it as type HTML so the template
		// execution knows not to re-encode it. we want to control the encoding
		// more carefully for making links of URLs, for one.
		items[i].DescriptionHTML = getHTMLDescription(items[i].Description)
	}

	// Get count of total feed items (all pages).
	totalItems, err := dbCountItems(db, userID, readState)
	if err != nil {
		log.Print(err)
		send500Error(rw, "Failed to lookup count.")
		return
	}

	totalPages := int(math.Ceil(float64(totalItems) / float64(pageSize)))
	nextPage := -1
	if page < totalPages {
		nextPage = page + 1
	}
	prevPage := page - 1

	// We may have messages to display.
	// Right now we only have success messages
	flashes := session.Flashes()
	var successMessages []string
	for _, flash := range flashes {
		// type assertion. flash is interface{}
		if str, ok := flash.(string); ok {
			successMessages = append(successMessages, str)
		}
	}

	err = session.Save(request, rw)
	if err != nil {
		log.Printf("Unable to save session: %s", err)
		send500Error(rw, "Failed to save your session.")
		return
	}

	// Show the page.

	type ListItemsPage struct {
		PageTitle        string
		Items            []gorselib.RSSItem
		Feeds            []gorselib.RSSFeed
		SuccessMessages  []string
		Path             string
		SortOrder        string
		ReverseSortOrder string
		TotalItems       int
		Page             int
		NextPage         int
		PreviousPage     int
		UserID           int
		ReadState        ReadState
		Unread           ReadState
		ReadLater        ReadState
	}

	listItemsPage := ListItemsPage{
		PageTitle:        "",
		Items:            items,
		Feeds:            feeds,
		SuccessMessages:  successMessages,
		Path:             request.URL.Path,
		SortOrder:        sortRaw,
		ReverseSortOrder: reverseSortOrder,
		TotalItems:       totalItems,
		Page:             page,
		NextPage:         nextPage,
		PreviousPage:     prevPage,
		UserID:           userID,
		ReadState:        readState,
		Unread:           Unread,
		ReadLater:        ReadLater,
	}

	err = renderPage(rw, "_list_items", listItemsPage)
	if err != nil {
		log.Printf("Failure rendering page: %s", err)
		send500Error(rw, "Failed to render page")
		return
	}
	log.Print("Rendered list items page.")
}

// handlerUpdateReadFlags handles an update read flags (item state) request.
//
// It implements the type RequestHandlerFunc
//
// We update the requested flags in the database, and then redirect us back to
// the list of items page.
func handlerUpdateReadFlags(rw http.ResponseWriter, request *http.Request,
	settings *GorseConfig, session *sessions.Session) {
	// We should have some posted request values. In order to get at these, we
	// have to run ParseForm().
	err := request.ParseForm()
	if err != nil {
		log.Printf("Failed to parse form: %s", err)
		send500Error(rw, "Failed to parse request")
		return
	}

	db, err := getDB(settings)
	if err != nil {
		log.Printf("Failed to get database connection: %s", err)
		send500Error(rw, "Failed to connect to database")
		return
	}

	userIDStr := request.PostForm.Get("user-id")
	if userIDStr == "" {
		log.Printf("No user ID in request.")
		send400Error(rw, "Incomplete request")
		return
	}
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		log.Printf("Bad user ID: %s: %s", userIDStr, err)
		send400Error(rw, "Bad user ID")
		return
	}

	// What read state were we viewing? This tells us where to go after.
	// We either view unread or read later items. Those marked read we never can
	// see again currently.
	readState := Unread
	requestedReadState := request.PostForm.Get("read-state")
	if requestedReadState == "read-later" {
		readState = ReadLater
	}

	// Set some read.

	// Check if we have any items to update. These are in the request key
	// 'read-item'.
	readItems, exists := request.PostForm["read-item"]
	readCount := 0
	if exists {
		// This is associated with a slice of strings. Each of these is an id we
		// want to mark as read now.
		for _, idStr := range readItems {
			var id int64
			id, err = strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				log.Printf("Failed to parse id into an integer %s: %s", idStr, err)
				send500Error(rw, "Invalid id")
				return
			}

			// Record it to the "read after archive" table if it was archived and now
			// is being flagged read.

			item, err := dbGetItem(db, id, userID)
			if err != nil {
				log.Printf("Unable to look up item: %d: %s", id, err)
				send500Error(rw, "Unable to look up item.")
				return
			}

			if item.ReadState == "read-later" {
				err := dbRecordReadAfterArchive(db, userID, item)
				if err != nil {
					log.Printf("Unable to record item read after archive: %d: %s", id, err)
					send500Error(rw, "Unable to read read after archive.")
					return
				}
			}

			// Flag it read.

			err = dbSetItemReadState(db, id, userID, Read)
			if err != nil {
				send500Error(rw, "Unable to update read flag for "+idStr)
				return
			}

			readCount++
		}
	}

	if readCount == 1 {
		log.Printf("Set %d item read.", readCount)
	} else {
		log.Printf("Set %d items read.", readCount)
	}

	// Set some archived.

	archiveItems, exists := request.PostForm["archive-item"]
	archivedCount := 0
	if exists {
		for _, idStr := range archiveItems {
			var id int64
			id, err = strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				log.Printf("Failed to parse id into an integer %s: %s", idStr, err)
				send500Error(rw, "Invalid id")
				return
			}

			err = dbSetItemReadState(db, id, userID, ReadLater)
			if err != nil {
				send500Error(rw, "Unable to update read flag for "+idStr)
				return
			}

			archivedCount++
		}
	}

	if archivedCount == 1 {
		log.Printf("Archived %d item.", archivedCount)
	} else {
		log.Printf("Archived %d items.", archivedCount)
	}

	session.AddFlash("Saved.")

	err = session.Save(request, rw)
	if err != nil {
		log.Printf("Unable to save session: %s", err)
		send500Error(rw, "Failed to save your session.")
		return
	}

	// TODO: Should we get path from the config?
	uri := fmt.Sprintf("/gorse/?sort-order=%s&user-id=%d&read-state=%s",
		url.QueryEscape(request.PostForm.Get("sort-order")),
		userID,
		url.QueryEscape(readState.String()))

	http.Redirect(rw, request, uri, http.StatusFound)
}

// handlerStaticFiles serves up some static files.
// it implements the type RequestHandlerFunc
// while it may be 'better' to serve these through a 'real' httpd, this
// simplifies setup, so support this method too.
func handlerStaticFiles(rw http.ResponseWriter, request *http.Request,
	settings *GorseConfig, session *sessions.Session) {
	log.Printf("Serving static request [%s]", request.URL.Path)

	// set the dir we serve.
	// TODO: possibly we should get this from a config and use an absolute
	//   path?
	var staticDir = http.Dir("./static")

	// create the fileserver handler that deals with the internals for us.
	var fileserverHandler = http.FileServer(staticDir)

	// we want to serve up the directory without the global uri prefix
	// since it is relative / may bare no resemblance to the request path.
	var strippedHandler = http.StripPrefix(settings.URIPrefix+"/static/",
		fileserverHandler)
	strippedHandler.ServeHTTP(rw, request)
}

// ServeHTTP handles an http request. it is invoked by the fastcgi
// package in a goroutine.
func (handler HTTPHandler) ServeHTTP(rw http.ResponseWriter,
	request *http.Request) {
	log.Printf("Serving [%s] request from [%s] to path [%s]", request.Method,
		request.RemoteAddr, request.URL.Path)

	// Get existing session, or make a new one.
	session, err := handler.sessionStore.Get(request, handler.settings.SessionName)
	if err != nil {
		log.Printf("Session Get error: %s", err)
		send500Error(rw, "Failed to get your session.")
		context.Clear(request)
		return
	}

	// We need to decide how to parse this request. We do this by looking at the
	// HTTP method and the path.

	type RequestHandlerFunc func(http.ResponseWriter, *http.Request,
		*GorseConfig, *sessions.Session)

	type RequestHandler struct {
		Method string

		// Regex pattern on the path to match.
		PathPattern string

		Func RequestHandlerFunc
	}

	var handlers = []RequestHandler{
		// GET /
		RequestHandler{
			Method:      "GET",
			PathPattern: "^" + handler.settings.URIPrefix + "/?$",
			Func:        handlerListItems,
		},

		// POST /update_read_flags
		RequestHandler{
			Method:      "POST",
			PathPattern: "^" + handler.settings.URIPrefix + "/update_read_flags/?$",
			Func:        handlerUpdateReadFlags,
		},

		// GET /static/*
		RequestHandler{
			Method:      "GET",
			PathPattern: "^" + handler.settings.URIPrefix + "/static/",
			Func:        handlerStaticFiles,
		},
	}

	// Find a matching handler.
	for _, actionHandler := range handlers {
		if actionHandler.Method != request.Method {
			continue
		}

		matched, err := regexp.MatchString(actionHandler.PathPattern,
			request.URL.Path)
		if err != nil {
			log.Printf("Error matching regex: %s", err)
			continue
		}

		if matched {
			actionHandler.Func(rw, request, handler.settings, session)
			// NOTE: We don't session.Save() here as if we redirect the Save()
			//   won't take effect.
			// Clean up gorilla globals. Sessions package says this must be done or
			// we'll leak memory.
			context.Clear(request)
			return
		}
	}

	// There was no matching handler. Send a 404.
	log.Printf("No handler for this request.")
	rw.WriteHeader(http.StatusNotFound)
	_, _ = rw.Write([]byte("<h1>404 Not Found</h1>"))
	_ = session.Save(request, rw)
	context.Clear(request)
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime)

	configPath := flag.String("config", "", "Path to a configuration file.")
	logPath := flag.String("log-file", "", "Path to a log file.")
	wwwPath := flag.String("www-path", "", "Path to directory containing assets. This directory must contain the static and templates directories. We change directory here at startup.")
	flag.Parse()

	if len(*configPath) == 0 {
		log.Print("You must specify a configuration file.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if len(*logPath) == 0 {
		log.Print("You must specify a log file.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if len(*wwwPath) == 0 {
		log.Print("You must specify a www path.")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Open log file. Don't use os.Create() because that truncates.
	logFh, err := os.OpenFile(*logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		log.Printf("Failed to open log file: %s: %s", *logPath, err)
		os.Exit(1)
	}
	log.SetOutput(logFh)

	// chdir to the www path so we can get what we need to serve up.
	err = os.Chdir(*wwwPath)
	if err != nil {
		log.Printf("Unable to chdir to www directory: %s: %s", *wwwPath, err)
		os.Exit(1)
	}

	var settings GorseConfig
	err = config.GetConfig(*configPath, &settings)
	if err != nil {
		log.Printf("Failed to retrieve config: %s", err)
		os.Exit(1)
	}

	var sessionStore = sessions.NewCookieStore(
		[]byte(settings.CookieAuthenticationKey))

	var listenHostPort = fmt.Sprintf("%s:%d", settings.ListenHost,
		settings.ListenPort)
	listener, err := net.Listen("tcp", listenHostPort)
	if err != nil {
		log.Printf("Failed to open port: %s", err)
		os.Exit(1)
	}

	var httpHandler = HTTPHandler{
		settings:     &settings,
		sessionStore: sessionStore,
	}

	// This will serve requests forever - should we have a signal or a method to
	// cause this to gracefully stop?
	log.Print("Starting to serve requests. (FastCGI)")
	err = fcgi.Serve(listener, httpHandler)
	if err != nil {
		log.Printf("Failed to start serving: %s", err)
		os.Exit(1)
	}
}

// Turn read state into the enumerated type in the database (read_state).
func (s ReadState) String() string {
	if s == Unread {
		return "unread"
	}
	if s == Read {
		return "read"
	}
	return "read-later"
}
