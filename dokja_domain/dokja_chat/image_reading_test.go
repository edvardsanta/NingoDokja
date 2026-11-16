package chat

import (
	"strings"
	"testing"
)

func TestImageReadingPlanSkipsNonImagesAndRejectsInvalidMetadata(t *testing.T) {
	attachments := []WebAttachment{
		{URL: "https://cdn.example/shot.png", Filename: "shot.png", ContentType: "image/png", Size: 900000},
		{URL: "https://cdn.example/clip.mp4", Filename: "clip.mp4", ContentType: "video/mp4", Size: 900000},
		{URL: "https://cdn.example/huge.png", Filename: "huge.png", ContentType: "image/png", Size: MaxImageReadingBytes + 1},
		{URL: "file:///etc/passwd", Filename: "fake.png", ContentType: "image/png"},
		{URL: "https://cdn.example/photo.jpg", Filename: "dir\\photo.jpg", ContentType: "IMAGE/JPEG", Size: 1},
		{URL: "https://cdn.example/shot.png", Filename: "dup.png", ContentType: "image/png", Size: 1},
	}
	requests := PlanImageReadings(attachments)
	if len(requests) != 4 {
		t.Fatal("video kept or duplicate read", requests)
	}
	if requests[0].Error != "" || requests[1].Error != "too_large" || requests[2].Error != "unreadable" || requests[3].Error != "" {
		t.Fatal(requests)
	}
	if requests[0].Filename != "shot.png" || requests[3].Filename != "photo.jpg" || requests[3].ContentType != "image/jpeg" {
		t.Fatal("filename or content type not normalized", requests)
	}
	requests = PlanImageReadings([]WebAttachment{{URL: strings.Repeat("x", 2049), Filename: "x.png", ContentType: "image/png"}})
	if len(requests) != 1 || requests[0].Error != "unreadable" {
		t.Fatal("unbounded image address")
	}
}
