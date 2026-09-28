package main

import (
	"database/sql"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB

func initDB() {
	dsn := "postgresql://postgres.zebevhrnhhrlpfsiqysf:ElderCare2026DB@aws-0-ap-northeast-1.pooler.supabase.com:5432/postgres"
	var err error
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("❌ เชื่อมต่อฐานข้อมูลล้มเหลว: %v", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatalf("❌ ไม่สามารถติดต่อฐานข้อมูล PostgreSQL ได้: %v", err)
	}
	_, err = db.Exec(`ALTER TABLE caregivers ADD COLUMN IF NOT EXISTS profile_image TEXT`)
	if err != nil {
		log.Printf("ไม่สามารถเตรียมคอลัมน์รูปโปรไฟล์ผู้ดูแล: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS login_audit (
		id BIGSERIAL PRIMARY KEY,
		email TEXT NOT NULL,
		provider TEXT NOT NULL DEFAULT 'password',
		success BOOLEAN NOT NULL,
		login_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		log.Printf("ไม่สามารถเตรียมตาราง login_audit: %v", err)
	}
	_, err = db.Exec(`ALTER TABLE medicine ADD COLUMN IF NOT EXISTS reminder_time TIME`)
	if err != nil {
		log.Printf("ไม่สามารถเตรียมเวลาแจ้งเตือนยา: %v", err)
	}
	_, err = db.Exec(`ALTER TABLE medicine ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`)
	if err != nil {
		log.Printf("ไม่สามารถเตรียมเวลาปรับปรุงรายการยา: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS watch_location (
		id BIGSERIAL PRIMARY KEY,
		device_id TEXT NOT NULL,
		latitude DOUBLE PRECISION NOT NULL,
		longitude DOUBLE PRECISION NOT NULL,
		recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		log.Printf("ไม่สามารถเตรียมตารางตำแหน่งนาฬิกา: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS elderly_profile (
		elderly_id BIGINT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		age INTEGER NOT NULL DEFAULT 0,
		blood_type TEXT NOT NULL DEFAULT '',
		diseases TEXT NOT NULL DEFAULT '',
		profile_image TEXT NOT NULL DEFAULT '',
		watch_device_id TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		log.Printf("ไม่สามารถเตรียมตารางโปรไฟล์ผู้สูงอายุ: %v", err)
	}
	log.Println("✅ เชื่อมต่อฐานข้อมูล PostgreSQL สำเร็จ!")
}

type HealthData struct {
	DeviceID  string  `json:"device_id"`
	BPM       int     `json:"bpm"`
	SpO2      int     `json:"spo2"`
	Battery   int     `json:"battery"`
	BP        string  `json:"bp"`
	IsFalling bool    `json:"is_falling"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Timestamp string  `json:"timestamp"`
}

type AlertData struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	HeartRate int    `json:"heart_rate"`
	Timestamp string `json:"timestamp"`
}

func recordLogin(email string, provider string, success bool) {
	if _, err := db.Exec(
		"INSERT INTO login_audit (email, provider, success) VALUES ($1, $2, $3)",
		email,
		provider,
		success,
	); err != nil {
		log.Printf("ไม่สามารถบันทึก login audit: %v", err)
	}
}

type Medicine struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Subtitle     string `json:"subtitle"`
	ReminderTime string `json:"reminder_time"`
	IsTaken      bool   `json:"is_taken"`
	UpdatedAt    string `json:"updated_at"`
}

type LocationData struct {
	DeviceID   string  `json:"device_id"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	RecordedAt string  `json:"recorded_at"`
}

type ElderlyProfile struct {
	ElderlyID     int    `json:"elderly_id"`
	Name          string `json:"name"`
	Age           int    `json:"age"`
	BloodType     string `json:"blood_type"`
	Diseases      string `json:"diseases"`
	ProfileImage  string `json:"profile_image"`
	WatchDeviceID string `json:"watch_device_id"`
	UpdatedAt     string `json:"updated_at"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type SocialLoginRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

type ForgotPasswordRequest struct {
	Email       string `json:"email"`
	NewPassword string `json:"new_password"`
}

type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

var jwtKey = []byte("YOUR_SUPER_SECRET_KEY_ELDERCARE")

var (
	currentData HealthData
	mutex       sync.Mutex
)

func main() {
	initDB()
	defer db.Close()

	app := fiber.New()

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept",
	}))

	// ==========================================
	// 📌 ส่วนที่ 1: ตรวจสอบสถานะเซิร์ฟเวอร์
	// ==========================================
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ElderCare API is running successfully with PostgreSQL & Supabase! 🚀")
	})

	// ==========================================
	// 📌 ส่วนที่ 2: ระบบสมาชิก & ยืนยันตัวตน (Auth API)
	// ==========================================
	app.Post("/api/login", func(c *fiber.Ctx) error {
		var req LoginRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid input"})
		}

		var storedPassword string
		var userName string
		query := "SELECT name, password FROM users WHERE email = $1"
		err := db.QueryRow(query, req.Email).Scan(&userName, &storedPassword)
		if err != nil {
			recordLogin(req.Email, "password", false)
			return c.Status(401).JSON(fiber.Map{"message": "ไม่พบอีเมลนี้ในระบบ"})
		}

		err = bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(req.Password))
		if err != nil {
			recordLogin(req.Email, "password", false)
			return c.Status(401).JSON(fiber.Map{"message": "รหัสผ่านไม่ถูกต้อง"})
		}

		expirationTime := time.Now().Add(24 * time.Hour)
		claims := &Claims{
			Email: req.Email,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(expirationTime),
			},
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString(jwtKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Could not generate token"})
		}
		recordLogin(req.Email, "password", true)

		return c.JSON(fiber.Map{
			"message": "เข้าสู่ระบบสำเร็จ",
			"token":   tokenString,
			"name":    userName,
		})
	})

	app.Post("/api/register", func(c *fiber.Ctx) error {
		var req RegisterRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid input"})
		}

		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", req.Email).Scan(&exists)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Database error"})
		}

		if exists {
			return c.Status(409).JSON(fiber.Map{"message": "อีเมลนี้ถูกใช้งานแล้ว"})
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Server error"})
		}

		query := "INSERT INTO users (email, password, name) VALUES ($1, $2, $3)"
		_, err = db.Exec(query, req.Email, string(hashedPassword), req.Name)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Failed to register"})
		}

		return c.JSON(fiber.Map{"message": "สมัครสมาชิกสำเร็จ"})
	})

	app.Post("/api/social-login", func(c *fiber.Ctx) error {
		var req SocialLoginRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid input"})
		}

		var userID int
		var userName string
		queryCheck := "SELECT id, name FROM users WHERE email = $1"
		err := db.QueryRow(queryCheck, req.Email).Scan(&userID, &userName)

		if err != nil {
			insertQuery := "INSERT INTO users (email, password, name) VALUES ($1, $2, $3) RETURNING id"
			err = db.QueryRow(insertQuery, req.Email, "SOCIAL_LOGIN_"+req.Provider, req.Name).Scan(&userID)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"message": "Database error"})
			}
			userName = req.Name
		}
		recordLogin(req.Email, req.Provider, true)
		expirationTime := time.Now().Add(24 * time.Hour)
		claims := &Claims{
			Email: req.Email,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(expirationTime),
			},
		}
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Could not generate token"})
		}

		return c.JSON(fiber.Map{
			"message": "เข้าสู่ระบบด้วย " + req.Provider + " สำเร็จ",
			"name":    userName,
			"user_id": userID,
			"token":   token,
		})
	})

	app.Post("/api/forgot-password", func(c *fiber.Ctx) error {
		var req ForgotPasswordRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid input"})
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Server error"})
		}

		query := "UPDATE users SET password = $1 WHERE email = $2"
		result, err := db.Exec(query, string(hashedPassword), req.Email)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"message": "Database error"})
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			return c.Status(404).JSON(fiber.Map{"message": "ไม่พบอีเมลนี้ในระบบ"})
		}

		return c.JSON(fiber.Map{"message": "เปลี่ยนรหัสผ่านสำเร็จ"})
	})
	// ==========================================
	// 📌 ส่วนที่ 2.1: อัปเดตโปรไฟล์ผู้ดูแล
	// ==========================================
	app.Put("/api/caregiver/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		var data struct {
			Name         string `json:"name"`
			Email        string `json:"email"`
			Phone        string `json:"phone"`
			ProfileImage string `json:"profile_image"`
		}
		if err := c.BodyParser(&data); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		query := "UPDATE caregivers SET name = $1, email = $2, phone = $3, profile_image = $4 WHERE id = $5"
		_, err := db.Exec(query, data.Name, data.Email, data.Phone, data.ProfileImage, id)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}

		return c.JSON(fiber.Map{"message": "อัปเดตโปรไฟล์ผู้ดูแลสำเร็จ"})
	})

	app.Get("/api/caregiver/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		var name, email, phone, profileImage string
		err := db.QueryRow(
			"SELECT name, email, phone, COALESCE(profile_image, '') FROM caregivers WHERE id = $1",
			id,
		).Scan(&name, &email, &phone, &profileImage)
		if err == sql.ErrNoRows {
			return c.Status(404).JSON(fiber.Map{"error": "Caregiver not found"})
		}
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}
		return c.JSON(fiber.Map{
			"name":          name,
			"email":         email,
			"phone":         phone,
			"profile_image": profileImage,
		})
	})

	app.Get("/api/elderly/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		var profile ElderlyProfile
		err := db.QueryRow(`SELECT elderly_id, name, age, blood_type, diseases,
			profile_image, watch_device_id, updated_at::text
			FROM elderly_profile WHERE elderly_id = $1`, id).Scan(
			&profile.ElderlyID,
			&profile.Name,
			&profile.Age,
			&profile.BloodType,
			&profile.Diseases,
			&profile.ProfileImage,
			&profile.WatchDeviceID,
			&profile.UpdatedAt,
		)
		if err == sql.ErrNoRows {
			return c.Status(404).JSON(fiber.Map{"error": "Elderly profile not found"})
		}
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}
		return c.JSON(profile)
	})

	app.Put("/api/elderly/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		var profile ElderlyProfile
		if err := c.BodyParser(&profile); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}
		_, err := db.Exec(`INSERT INTO elderly_profile (
			elderly_id, name, age, blood_type, diseases, profile_image, watch_device_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (elderly_id) DO UPDATE SET
			name = EXCLUDED.name,
			age = EXCLUDED.age,
			blood_type = EXCLUDED.blood_type,
			diseases = EXCLUDED.diseases,
			profile_image = EXCLUDED.profile_image,
			watch_device_id = EXCLUDED.watch_device_id,
			updated_at = NOW()`,
			id,
			profile.Name,
			profile.Age,
			profile.BloodType,
			profile.Diseases,
			profile.ProfileImage,
			profile.WatchDeviceID,
		)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to save elderly profile"})
		}
		return c.JSON(fiber.Map{"message": "บันทึกข้อมูลผู้สูงอายุสำเร็จ"})
	})

	// ==========================================
	// 📌 ส่วนที่ 3: ข้อมูลสุขภาพ & SOS (HealthData)
	// ==========================================
	app.Get("/api/health", func(c *fiber.Ctx) error {
		mutex.Lock()
		defer mutex.Unlock()
		return c.JSON(currentData)
	})

	app.Post("/api/health", func(c *fiber.Ctx) error {
		var newData HealthData
		if err := c.BodyParser(&newData); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		if newData.Timestamp == "" {
			newData.Timestamp = time.Now().Format("2006-01-02 15:04:05")
		}

		mutex.Lock()
		currentData = newData
		mutex.Unlock()

		query := "INSERT INTO health_data (elderly_id, heart_rate, blood_oxygen, blood_pressure, record_timestamp) VALUES ($1, $2, $3, $4, $5)"
		_, err := db.Exec(query, 1, newData.BPM, newData.SpO2, newData.BP, newData.Timestamp)
		if err != nil {
			log.Printf("❌ บันทึก Health Data ลง DB ไม่สำเร็จ: %v", err)
		}

		return c.JSON(fiber.Map{"status": "success", "data": currentData})
	})

	app.Get("/api/health/history", func(c *fiber.Ctx) error {
		rows, err := db.Query(`SELECT heart_rate, record_timestamp::text
			FROM health_data WHERE elderly_id = $1
			ORDER BY record_timestamp DESC LIMIT 168`, 1)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}
		defer rows.Close()

		var history []HealthData
		for rows.Next() {
			var item HealthData
			if err := rows.Scan(&item.BPM, &item.Timestamp); err == nil {
				history = append(history, item)
			}
		}
		if history == nil {
			history = []HealthData{}
		}
		return c.JSON(history)
	})

	app.Post("/api/location", func(c *fiber.Ctx) error {
		var location LocationData
		if err := c.BodyParser(&location); err != nil || location.DeviceID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid location"})
		}
		if location.RecordedAt == "" {
			location.RecordedAt = time.Now().Format(time.RFC3339)
		}
		_, err := db.Exec(`INSERT INTO watch_location (device_id, latitude, longitude, recorded_at)
			VALUES ($1, $2, $3, $4)`, location.DeviceID, location.Latitude, location.Longitude, location.RecordedAt)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to save location"})
		}
		return c.Status(201).JSON(location)
	})

	app.Get("/api/location/latest", func(c *fiber.Ctx) error {
		deviceID := c.Query("device_id")
		if deviceID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "device_id is required"})
		}
		var location LocationData
		err := db.QueryRow(`SELECT device_id, latitude, longitude, recorded_at::text
			FROM watch_location WHERE device_id = $1 ORDER BY recorded_at DESC LIMIT 1`, deviceID).
			Scan(&location.DeviceID, &location.Latitude, &location.Longitude, &location.RecordedAt)
		if err == sql.ErrNoRows {
			return c.Status(404).JSON(fiber.Map{"error": "Location not found"})
		}
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}
		return c.JSON(location)
	})

	app.Post("/api/sos", func(c *fiber.Ctx) error {
		mutex.Lock()
		currentData.IsFalling = true
		mutex.Unlock()
		return c.JSON(fiber.Map{"status": "sos_triggered"})
	})

	// ==========================================
	// 📌 ส่วนที่ 4: ประวัติการแจ้งเตือน (Alert & History)
	// ==========================================
	app.Post("/api/alert", func(c *fiber.Ctx) error {
		var data AlertData
		if err := c.BodyParser(&data); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		if data.Timestamp == "" {
			data.Timestamp = time.Now().Format("2006-01-02 15:04:05")
		}

		query := "INSERT INTO emergency_alert (elderly_id, alert_type, title, heart_rate, alert_timestamp) VALUES ($1, $2, $3, $4, $5)"
		_, err := db.Exec(query, 1, data.Type, data.Title, data.HeartRate, data.Timestamp)
		if err != nil {
			log.Printf("❌ บันทึก Alert ลง DB ไม่สำเร็จ: %v", err)
		}

		return c.SendStatus(200)
	})

	app.Get("/api/history", func(c *fiber.Ctx) error {
		rows, err := db.Query("SELECT alert_id, alert_type, title, heart_rate, alert_timestamp FROM emergency_alert ORDER BY alert_id DESC LIMIT 50")
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}
		defer rows.Close()

		var historyList []AlertData
		for rows.Next() {
			var item AlertData
			if err := rows.Scan(&item.ID, &item.Type, &item.Title, &item.HeartRate, &item.Timestamp); err != nil {
				continue
			}
			historyList = append(historyList, item)
		}

		if historyList == nil {
			return c.JSON([]AlertData{})
		}
		return c.JSON(historyList)
	})

	app.Delete("/api/history/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		result, err := db.Exec("DELETE FROM emergency_alert WHERE alert_id = $1", id)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}
		deleted, _ := result.RowsAffected()
		if deleted == 0 {
			return c.Status(404).JSON(fiber.Map{"error": "Alert not found"})
		}
		return c.SendStatus(204)
	})

	// ==========================================
	// 📌 ส่วนที่ 5: จัดการรายการยา (Medicine API)
	// ==========================================
	app.Get("/api/medicines/:elderly_id", func(c *fiber.Ctx) error {
		elderlyID := c.Params("elderly_id")
		rows, err := db.Query(`SELECT id, title, subtitle, COALESCE(reminder_time::text, ''), is_taken, updated_at::text
			FROM medicine WHERE elderly_id = $1 ORDER BY reminder_time NULLS LAST, id`, elderlyID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Database error"})
		}
		defer rows.Close()

		var meds []Medicine
		for rows.Next() {
			var m Medicine
			if err := rows.Scan(&m.ID, &m.Title, &m.Subtitle, &m.ReminderTime, &m.IsTaken, &m.UpdatedAt); err != nil {
				continue
			}
			meds = append(meds, m)
		}

		if meds == nil {
			return c.JSON([]Medicine{})
		}
		return c.JSON(meds)
	})

	app.Post("/api/medicines", func(c *fiber.Ctx) error {
		var m Medicine
		if err := c.BodyParser(&m); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}

		query := `INSERT INTO medicine (elderly_id, title, subtitle, reminder_time, is_taken)
			VALUES ($1, $2, $3, NULLIF($4, '')::time, $5)`
		_, err := db.Exec(query, 1, m.Title, m.Subtitle, m.ReminderTime, m.IsTaken)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to save medicine"})
		}

		return c.JSON(fiber.Map{"status": "success", "message": "เพิ่มรายการยาสำเร็จ"})
	})

	app.Put("/api/medicines/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		var m Medicine
		if err := c.BodyParser(&m); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
		}
		result, err := db.Exec(`UPDATE medicine SET title = $1, subtitle = $2,
			reminder_time = NULLIF($3, '')::time, is_taken = $4, updated_at = NOW() WHERE id = $5`,
			m.Title, m.Subtitle, m.ReminderTime, m.IsTaken, id)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to update medicine"})
		}
		updated, _ := result.RowsAffected()
		if updated == 0 {
			return c.Status(404).JSON(fiber.Map{"error": "Medicine not found"})
		}
		return c.JSON(fiber.Map{"status": "success", "message": "อัปเดตรายการยาสำเร็จ"})
	})

	app.Delete("/api/medicines/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		query := "DELETE FROM medicine WHERE id = $1"
		_, err := db.Exec(query, id)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to delete medicine"})
		}

		return c.JSON(fiber.Map{"status": "success", "message": "ลบรายการยาสำเร็จ"})
	})

	// ==========================================
	// 📌 ส่วนที่ 6: รันเซิร์ฟเวอร์ (Render Port)
	// ==========================================
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server is starting on port %s...", port)
	log.Fatal(app.Listen("0.0.0.0:" + port))
}
