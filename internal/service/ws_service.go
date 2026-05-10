package service

import "github.com/nicoflow/nicoflow-api/internal/ws"

type WSService struct {
	hub *ws.Hub
}

func NewWSService(hub *ws.Hub) *WSService {
	return &WSService{hub: hub}
}

func (s *WSService) Hub() *ws.Hub {
	return s.hub
}
