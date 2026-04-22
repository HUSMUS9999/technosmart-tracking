package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

type Technician struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	Phone     string    `json:"phone"`
	IsActive  bool      `gorm:"default:true" json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SystemConfig struct {
	Key   string `gorm:"primaryKey" json:"key"`
	Value string `gorm:"type:text" json:"value"`
}

func InitDB() error {
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" { dbHost = "localhost" }
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" { dbUser = "moca_admin" }
	dbPass := os.Getenv("DB_PASSWORD")
	if dbPass == "" { dbPass = "changeme" }
	dbName := os.Getenv("DB_NAME")
	if dbName == "" { dbName = "moca_tracker" }
	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" { dbPort = "5432" }

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Europe/Paris",
		dbHost, dbUser, dbPass, dbName, dbPort)

	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database using GORM: %w", err)
	}

	// Migrate the schema
	err = database.AutoMigrate(&Technician{}, &SystemConfig{})
	if err != nil {
		return fmt.Errorf("failed to migrate schema: %w", err)
	}

	DB = database
	log.Println("[db] GORM PostgreSQL initialized and migrated")
	return nil
}

// GetTechniciansMap returns a clean mapping of name to phone for compatibility with old config.
func GetTechniciansMap() map[string]string {
	var techs []Technician
	DB.Where("is_active = ?", true).Find(&techs)
	res := make(map[string]string)
	for _, t := range techs {
		res[t.Name] = t.Phone
	}
	return res
}

// EnsureTechnician creates or updates a technician
func EnsureTechnician(name string, phone string) error {
	var tech Technician
	err := DB.Where("name = ?", name).First(&tech).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			tech = Technician{Name: name, Phone: phone, IsActive: true}
			return DB.Create(&tech).Error
		}
		return err
	}
	
	// Update phone if different
	if tech.Phone != phone {
		tech.Phone = phone
		return DB.Save(&tech).Error
	}
	return nil
}

func DeleteTechnician(name string) error {
	return DB.Where("name = ?", name).Delete(&Technician{}).Error
}
