package model

import "time"

type FolderIcons string

const (
	FolderIconInbox    FolderIcons = "inbox"
	FolderIconCalendar FolderIcons = "calendar"
	FolderIconAlarm    FolderIcons = "alarm"
	FolderIconSearch   FolderIcons = "search"
	FolderIconSettings FolderIcons = "settings"
	FolderIconMenu     FolderIcons = "menu"
	FolderIconFolder   FolderIcons = "folder"
	FolderIconLayer    FolderIcons = "layer"
	FolderIconZap      FolderIcons = "zap"
	FolderIconComputer FolderIcons = "computer"
	FolderIconUser     FolderIcons = "user"
	FolderIconSprout   FolderIcons = "sprout"
	FolderIconBone     FolderIcons = "bone"
)

type Project struct {
	ID           string      `json:"id"`
	UserID       string      `json:"user_id"`
	AreaID       *string     `json:"area_id,omitempty"`
	Name         string      `json:"name"`
	Status       string      `json:"status"`
	FolderIcon   FolderIcons `json:"folder_icon"`
	DisplayOrder int         `json:"display_order"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}
