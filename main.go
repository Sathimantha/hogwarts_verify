package main

import (
	"database/sql"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"regexp"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

var db *sql.DB

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	dbUser := os.Getenv("DB_USERNAME")
	dbPass := os.Getenv("DB_PASSWORD")
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbName := os.Getenv("DB_NAME")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", dbUser, dbPass, dbHost, dbPort, dbName)
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.Close()

	r := mux.NewRouter()
	r.HandleFunc("/verify", verifyHandler).Methods("GET")

	// Add CORS middleware
	corsHandler := handlers.CORS(
		handlers.AllowedOrigins([]string{"https://hogwarts-legacy.info"}),
		handlers.AllowedMethods([]string{"GET"}),
		handlers.AllowedHeaders([]string{"Content-Type", "Accept"}),
	)(r)

	certFile := os.Getenv("CERT_FILE")
	keyFile := os.Getenv("KEY_FILE")
	if certFile == "" || keyFile == "" {
		log.Fatalf("CERT_FILE or KEY_FILE not defined in .env")
	}

	log.Println("Server started on :5001 with SSL")
	err = http.ListenAndServeTLS(":5001", certFile, keyFile, corsHandler)
	if err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func verifyHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	// Validate ID format (alphanumeric + optional hyphen, max 50 chars)
	if len(id) > 50 || !regexp.MustCompile(`^[a-zA-Z0-9\-]+$`).MatchString(id) {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	var fullName, category, remark string
	query := `SELECT full_name, category, remark FROM people WHERE national_id = ? LIMIT 1`
	err := db.QueryRow(query, id).Scan(&fullName, &category, &remark)
	if err == sql.ErrNoRows {
		http.Error(w, "Person not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		log.Printf("Database error: %v", err)
		return
	}

	// Escape values to prevent XSS
	safeID := html.EscapeString(id)
	safeName := html.EscapeString(fullName)
	safeRemark := html.EscapeString(remark)

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
		<div style="margin-top: 8px;">%s</div>
	</div>`, safeID, safeName, safeRemark)
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(htmlResponse))
}
