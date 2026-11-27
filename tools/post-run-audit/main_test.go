package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuditPairProvesSamsungPayload(t *testing.T) {
	root := t.TempDir()
	photo := filepath.Join(root, "sample.jpg")
	video := filepath.Join(root, "sample.mp4")
	payload := []byte("0123456789-video-payload-abcdefghijklmnopqrstuvwxyz")
	metadata := []byte(`<x:xmpmeta GCamera:MotionPhoto="1" GCamera:MotionPhotoVersion="1"><rdf:li Item:Mime="video/quicktime" Item:Semantic="MotionPhoto" Item:Length="126"/></x:xmpmeta>`)
	prefix := append([]byte{0xff, 0xd8, 0xff}, metadata...)
	prefix = append(prefix, []byte("MotionPhoto_Data")...)
	photoData := append(prefix, payload...)
	photoData = append(photoData, []byte("trailer")...)
	if err := os.WriteFile(photo, photoData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	result := auditPair(root, motionItem{
		Status: "merged", SourceStillPath: photo, SourceVideoPath: video,
		StillSize: int64(len(photoData)), VideoSize: int64(len(payload)),
	}, 1)
	if result.reason != "exact-byte-match" {
		t.Fatalf("reason=%s error=%s", result.reason, result.record.Error)
	}
	if result.record.VideoSHA256 == "" || result.record.VideoSHA256 != result.record.EmbeddedSHA256 {
		t.Fatal("exact payload hashes missing or different")
	}
	wantOffset := int64(len(prefix))
	if result.record.EmbeddedOffset != wantOffset {
		t.Fatalf("offset=%d want=%d", result.record.EmbeddedOffset, wantOffset)
	}
}

func TestAuditPairKeepsHEICWhenEmbeddedSizeDiffers(t *testing.T) {
	root := t.TempDir()
	photo := filepath.Join(root, "sample.heic")
	video := filepath.Join(root, "sample.mov")
	payload := []byte("video")
	metadata := []byte(`<x:xmpmeta GCamera:MotionPhoto="1" GCamera:MotionPhotoVersion="1"><rdf:li Item:Mime="video/quicktime" Item:Semantic="MotionPhoto" Item:Length="99"/></x:xmpmeta>`)
	photoData := append(metadata, []byte("image-data-longer-than-video")...)
	if err := os.WriteFile(photo, photoData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	result := auditPair(root, motionItem{
		Status: "merged", SourceStillPath: photo, SourceVideoPath: video,
		StillSize: int64(len(photoData)), VideoSize: int64(len(payload)),
	}, 1)
	if result.reason != "embedded-size-differs" {
		t.Fatalf("reason=%s error=%s", result.reason, result.record.Error)
	}
}

func TestAuditPairKeepsSharedVideo(t *testing.T) {
	root := t.TempDir()
	result := auditPair(root, motionItem{
		Status: "merged", SourceStillPath: filepath.Join(root, "same.jpg"), SourceVideoPath: filepath.Join(root, "same.mp4"),
	}, 2)
	if result.reason != "shared-video" {
		t.Fatalf("unexpected reason %s", result.reason)
	}
}
