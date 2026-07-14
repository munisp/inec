package main

import (
"log"
"os"
"time"

"gorm.io/driver/postgres"
"gorm.io/gorm"
gormlogger "gorm.io/gorm/logger"
)

var gormDB *gorm.DB

// Models
type User struct {
ID              uint   `gorm:"primaryKey"`
Username        string `gorm:"uniqueIndex;not null"`
PasswordHash    string `gorm:"not null"`
FullName        string `gorm:"not null"`
Role            string `gorm:"not null"`
StaffID         string `gorm:"uniqueIndex"`
StateCode       string
LgaCode         string
PollingUnitCode string
CreatedAt       time.Time `gorm:"autoCreateTime"`
IsActive        int       `gorm:"default:1"`
}

type Election struct {
ID                    uint      `gorm:"primaryKey"`
Title                 string    `gorm:"not null"`
ElectionType          string    `gorm:"not null"`
ElectionDate          string    `gorm:"not null"`
Status                string    `gorm:"not null;default:'upcoming'"`
Description           string
TotalRegisteredVoters int       `gorm:"default:0"`
CreatedAt             time.Time `gorm:"autoCreateTime"`
UpdatedAt             time.Time `gorm:"autoUpdateTime"`
}

type Party struct {
ID           uint   `gorm:"primaryKey"`
Code         string `gorm:"uniqueIndex;not null"`
Name         string `gorm:"not null"`
Abbreviation string `gorm:"not null"`
LogoURL      string
Color        string
IsActive     int `gorm:"default:1"`
}

func initGORM(dsn string) {
newLogger := gormlogger.New(
log.New(os.Stdout, "\r\n", log.LstdFlags),
gormlogger.Config{
SlowThreshold:             time.Second,
LogLevel:                  gormlogger.Warn,
IgnoreRecordNotFoundError: true,
Colorful:                  true,
},
)

var err error
gormDB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
Logger: newLogger,
})
if err != nil {
log.Printf("Failed to connect to database via GORM: %v", err)
return
}

// Auto Migrate
err = gormDB.AutoMigrate(
&User{},
&Election{},
&Party{},
)
if err != nil {
log.Printf("GORM AutoMigrate failed: %v", err)
} else {
log.Println("GORM AutoMigrate completed successfully")
}
}
