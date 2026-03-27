package service

import "context"

type AttachmentService struct {
	bucket string
}

func NewAttachmentService(bucket string) *AttachmentService {
	return &AttachmentService{bucket: bucket}
}

func (s *AttachmentService) UploadURL(ctx context.Context, userID, filename string) (string, error) {
	panic("not implemented")
}

func (s *AttachmentService) DownloadURL(ctx context.Context, userID, key string) (string, error) {
	panic("not implemented")
}
