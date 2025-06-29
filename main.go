package main

import (
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

var db *sql.DB
var digitRegex = regexp.MustCompile(`^\d+$`)

// charToWord maps characters to their spoken form for digit-by-digit reading
var charToWord = map[rune]string{
	'0': "zero",
	'1': "one",
	'2': "two",
	'3': "three",
	'4': "four",
	'5': "five",
	'6': "six",
	'7': "seven",
	'8': "eight",
	'9': "nine",
	'v': "vee",
	'V': "vee",
}

// stripHTML removes HTML tags and converts <br> to periods for natural speech
func stripHTML(input string) string {
	// Replace <br> with periods
	input = strings.ReplaceAll(input, "<br>", ". ")
	// Simple regex to remove HTML tags
	re := regexp.MustCompile(`<[^>]+>`)
	clean := re.ReplaceAllString(input, "")
	// Remove extra spaces and normalize
	clean = strings.TrimSpace(clean)
	clean = regexp.MustCompile(`\s+`).ReplaceAllString(clean, " ")
	return clean
}

// logError inserts an entry into the errors table
func logError(errorType, remark string) {
	// Use London timezone (UTC+1 for BST in June)
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		// Fallback to UTC if timezone loading fails
		timestamp := time.Now().UTC()
		_, dbErr := db.Exec("INSERT INTO errors (timestamp, error_type, remark) VALUES (?, ?, ?)", timestamp, errorType, fmt.Sprintf("Timezone error: %v; %s", err, remark))
		if dbErr != nil {
			// Silent fail to avoid disrupting response
		}
		return
	}
	timestamp := time.Now().In(london)

	query := `INSERT INTO errors (timestamp, error_type, remark) VALUES (?, ?, ?)`
	_, err = db.Exec(query, timestamp, errorType, remark)
	if err != nil {
		// Silent fail to avoid disrupting response
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		logError("STARTUP_ERROR", fmt.Sprintf("Error loading .env file: %v", err))
		os.Exit(1)
	}

	dbUser := os.Getenv("DB_USERNAME")
	dbPass := os.Getenv("DB_PASSWORD")
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbName := os.Getenv("DB_NAME")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", dbUser, dbPass, dbHost, dbPort, dbName)
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		logError("DB_CONNECTION_ERROR", fmt.Sprintf("Failed to connect to DB: %v", err))
		os.Exit(1)
	}
	defer db.Close()

	r := mux.NewRouter()

	// Define routes
	r.HandleFunc("/verify", verifyHandler).Methods("GET")
	r.HandleFunc("/twilio/verify", twilioVerifyHandler).Methods("POST")

	// Apply CORS only to /verify for frontend
	corsHandler := handlers.CORS(
		handlers.AllowedOrigins([]string{"https://hogwarts-legacy.info"}),
		handlers.AllowedMethods([]string{"GET", "POST"}),
		handlers.AllowedHeaders([]string{"Content-Type", "Accept"}),
	)

	// Wrap the entire router with CORS handler
	http.Handle("/", corsHandler(r))

	certFile := os.Getenv("CERT_FILE")
	keyFile := os.Getenv("KEY_FILE")
	if certFile == "" || keyFile == "" {
		logError("CONFIG_ERROR", "CERT_FILE or KEY_FILE not defined in .env")
		os.Exit(1)
	}

	err = http.ListenAndServeTLS(":5001", certFile, keyFile, nil)
	if err != nil {
		logError("SERVER_ERROR", fmt.Sprintf("Server failed: %v", err))
		os.Exit(1)
	}
}

func twilioVerifyHandler(w http.ResponseWriter, r *http.Request) {
	// Parse form data
	if err := r.ParseForm(); err != nil {
		logError("TWILIO_INVALID_FORM", fmt.Sprintf("Failed to parse form data: %v", err))
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	// Extract input from direct form fields (case-insensitive)
	input := r.PostFormValue("Digits")
	if input == "" {
		input = r.PostFormValue("digits")
	}
	if input == "" {
		input = r.PostFormValue("SpeechResult")
	}
	if input == "" {
		input = r.PostFormValue("speechresult")
	}

	// Fallback: Check if 'body' parameter contains Digits and SpeechResult
	if input == "" {
		body := r.PostFormValue("body")
		if body != "" {
			// Remove leading '?' if present
			body = strings.TrimPrefix(body, "?")
			// Decode URL-encoded body
			parsed, err := url.ParseQuery(body)
			if err != nil {
				logError("TWILIO_INVALID_BODY", fmt.Sprintf("Failed to parse body parameter: %v", err))
				http.Error(w, "Invalid body parameter", http.StatusBadRequest)
				return
			}
			input = parsed.Get("Digits")
			if input == "" {
				input = parsed.Get("digits")
			}
			if input == "" {
				input = parsed.Get("SpeechResult")
			}
			if input == "" {
				input = parsed.Get("speechresult")
			}
		}
	}

	if input == "" {
		logError("TWILIO_NO_INPUT", "No input provided in Digits or SpeechResult")
		http.Error(w, "No input provided", http.StatusBadRequest)
		return
	}

	// Remove spaces and normalize input
	input = strings.ReplaceAll(input, " ", "")

	// Validate input (alphanumeric, max 50 chars)
	if len(input) > 50 || !regexp.MustCompile(`^[a-zA-Z0-9]+$`).MatchString(input) {
		w.Header().Set("Content-Type", "application/xml")
		twiml := `<?xml version="1.0" encoding="UTF-8"?>
<Response>
	<Say>Invalid input format. Please use only numbers or letters.</Say>
</Response>`
		logError("TWILIO_INVALID_INPUT", fmt.Sprintf("Invalid input format: %s", input))
		w.Write([]byte(twiml))
		return
	}

	// Convert input to digit-by-digit spoken form
	var spokenInput []string
	for _, char := range input {
		if word, exists := charToWord[char]; exists {
			spokenInput = append(spokenInput, word)
		} else {
			spokenInput = append(spokenInput, string(char))
		}
	}
	spokenInputStr := strings.Join(spokenInput, " ")

	var fullName, category, remark string
	// Use LIKE to match input with or without trailing 'v'
	queryStr := `SELECT full_name, category, remark FROM people WHERE national_id LIKE ? LIMIT 1`
	err := db.QueryRow(queryStr, input+"%").Scan(&fullName, &category, &remark)
	if err != nil {
		logError("TWILIO_DB_ERROR", fmt.Sprintf("Database error for input %s: %v", input, err))
	}

	w.Header().Set("Content-Type", "application/xml")
	if err == nil {
		// Adjust category text for natural speech
		categoryText := "student"
		if category == "staff" {
			categoryText = "staff member"
		}
		// Clean remark by removing HTML tags
		cleanRemark := stripHTML(remark)
		// Generate TwiML with digit-by-digit input, name, category, and remark
		twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
	<Say>You entered %s. The name is %s. The category is %s. Remark: %s.</Say>
</Response>`, spokenInputStr, fullName, categoryText, cleanRemark)
		logError("TWILIO_SUCCESS", fmt.Sprintf("Verified input: %s, Name: %s, Category: %s, Remark: %s", input, fullName, categoryText, cleanRemark))
		w.Write([]byte(twiml))
	} else {
		// Generate TwiML for no match, including digit-by-digit input
		twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
	<Say>Sorry, no match found for %s.</Say>
</Response>`, spokenInputStr)
		logError("TWILIO_NO_MATCH", fmt.Sprintf("No match found for input: %s", input))
		w.Write([]byte(twiml))
	}
}

func isDigits(s string) bool {
	return digitRegex.MatchString(s)
}

func verifyHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		logError("VERIFY_NO_ID", "No ID provided in query parameter")
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	// Validate ID format (alphanumeric, max 50 chars)
	if len(id) > 50 || !regexp.MustCompile(`^[a-zA-Z0-9]+$`).MatchString(id) {
		logError("VERIFY_INVALID_ID", fmt.Sprintf("Invalid ID format: %s", id))
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	var fullName, category, remark string
	query := `SELECT full_name, category, remark FROM people WHERE national_id = ? LIMIT 1`
	err := db.QueryRow(query, id).Scan(&fullName, &category, &remark)
	if err == sql.ErrNoRows {
		logError("VERIFY_NOT_FOUND", fmt.Sprintf("Person not found for ID: %s", id))
		http.Error(w, "Person not found", http.StatusNotFound)
		return
	} else if err != nil {
		logError("VERIFY_DB_ERROR", fmt.Sprintf("Database error for ID %s: %v", id, err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Escape values to prevent XSS
	safeID := html.EscapeString(id)
	safeName := html.EscapeString(fullName)

	var htmlResponse string
	if category == "student" {
		htmlResponse = fmt.Sprintf(`<div style="font-family: Arial, sans-serif; line-height: 1.6; padding: 10px;">
			<strong>ID:</strong> %s<br>
			<strong>FULL NAME:</strong> %s<br>
			<strong>COURSES COMPLETED:</strong><br>
			<ul>
				<li>Introduction to Basic Psychology (One Hour Workshop)</li>
				<li>Introduction to Career Guidance (One Hour Workshop)</li>
				<li>Introduction to Basic Counselling (One Hour Workshop)</li>
				<li>Introduction to Basic IT (One Hour Workshop)</li>
				<li>Introduction to Basic Business Management (One Hour Workshop)</li>
				<li>Introduction to Basic Spoken English (One Hour Workshop)</li>
				<li>Introduction to Memory Boosting (One Hour Workshop)</li>
				<li>Introduction to Basic Personality Development (One Hour Workshop)</li>
				<li>Introduction to Entrepreneurship (One Hour Workshop)</li>
				<li>Introduction to Basic Body Language (One Hour Workshop)</li>
				<li>Introduction to Basic Counselling Skills (One Hour Workshop)</li>
				<li>Introduction to Basic Human Resource Management (One Hour Workshop)</li>
				<li>Introduction to Basic Teaching Methodologies (One Hour Workshop)</li>
				<li>Introduction to Basic Marketing Management (One Hour Workshop)</li>
			</ul>
			<strong>APPROVED AND VERIFIED:</strong> YES
		</div>`, safeID, safeName)
	} else {
		htmlResponse = fmt.Sprintf(`<div style="font-family: Arial, sans-serif; line-height: 1.6; padding: 10px;">
			<strong>ID:</strong> %s<br>
			<strong>FULL NAME:</strong> %s<br>
			<strong>REMARKS:</strong><br>
			%s
		</div>`, safeID, safeName, remark)
	}

	logError("VERIFY_SUCCESS", fmt.Sprintf("Verified ID: %s, Name: %s, Category: %s, Remark: %s", id, fullName, category, remark))
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(htmlResponse))
}
