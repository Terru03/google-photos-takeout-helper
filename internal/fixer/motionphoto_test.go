package fixer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateMotionPhotoDependenciesReturnsToolPath(t *testing.T) {
	toolPath := withFakeMotionPhotoTool(t, nil)

	info, err := ValidateMotionPhotoDependencies(ProcessOptions{CreateMotionPhotos: true})
	if err != nil {
		t.Fatalf("ValidateMotionPhotoDependencies returned error: %v", err)
	}
	if info == nil {
		t.Fatal("expected motion photo tool info")
	}
	if info.Path != toolPath {
		t.Fatalf("expected tool path %q, got %q", toolPath, info.Path)
	}
}

func TestValidateMotionPhotoDependenciesSkipsDryRun(t *testing.T) {
	info, err := ValidateMotionPhotoDependencies(ProcessOptions{
		CreateMotionPhotos: true,
		DryRun:             true,
	})
	if err != nil {
		t.Fatalf("ValidateMotionPhotoDependencies returned error: %v", err)
	}
	if info != nil {
		t.Fatalf("expected nil tool info during dry run, got %+v", info)
	}
}

func TestRunMotionPhotoPassInvokesSingleFileMode(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "motionphoto.args")
	imagePath := filepath.Join(t.TempDir(), "PXL_0001.jpg")
	videoPath := filepath.Join(t.TempDir(), "PXL_0001.mp4")
	if err := os.WriteFile(imagePath, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	imageHash, err := HashFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}

	withFakeMotionPhotoTool(t, map[string]string{
		"FAKE_MOTIONPHOTO_ARGS_FILE": argsFile,
		"FAKE_MOTIONPHOTO_STDOUT":    "processed",
		"FAKE_MOTIONPHOTO_APPEND_TO": imagePath,
	})

	result := RunMotionPhotoPass([]motionPhotoCleanupTarget{{
		videoOutputPath: videoPath,
		imageOutputPath: imagePath,
		imageHashBefore: imageHash,
	}}, ProcessOptions{CreateMotionPhotos: true})
	if result.Status != MotionPhotoPassStatusCompleted {
		t.Fatalf("expected completed status, got %+v", result)
	}

	args := readFileString(t, argsFile)
	requireContainsArg(t, args, "--input-image", imagePath)
	requireContainsArg(t, args, "--input-video", videoPath)
	requireContains(t, args, "--overwrite")
	requireNotContains(t, args, "--output-file")
}

func TestCleanupEmbeddedMotionPhotoVideosSkipsUnchangedImage(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "PXL_0001.jpg")
	videoPath := filepath.Join(root, "PXL_0001.mp4")

	if err := os.WriteFile(imagePath, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	imageHash, err := HashFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}

	deleted, skipped, cleanupErrors := CleanupEmbeddedMotionPhotoVideos([]motionPhotoCleanupTarget{{
		videoOutputPath: videoPath,
		imageOutputPath: imagePath,
		imageHashBefore: imageHash,
	}})
	if deleted != 0 {
		t.Fatalf("expected no deletions, got %d", deleted)
	}
	if skipped != 1 {
		t.Fatalf("expected 1 skipped file, got %d", skipped)
	}
	if cleanupErrors != 0 {
		t.Fatalf("expected no cleanup errors, got %d", cleanupErrors)
	}
	if !FileExists(videoPath) {
		t.Fatal("expected standalone video to stay when image did not change")
	}
}

func TestCleanupEmbeddedMotionPhotoVideosDeletesAlreadyEmbeddedVideo(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "PXL_0001.jpg")
	videoPath := filepath.Join(root, "PXL_0001.mp4")

	if err := os.WriteFile(imagePath, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	imageHash, err := HashFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}

	deleted, skipped, cleanupErrors := CleanupEmbeddedMotionPhotoVideos([]motionPhotoCleanupTarget{{
		videoOutputPath:          videoPath,
		imageOutputPath:          imagePath,
		imageHashBefore:          imageHash,
		deleteWithoutImageChange: true,
	}})
	if deleted != 1 {
		t.Fatalf("expected 1 deletion, got %d", deleted)
	}
	if skipped != 0 {
		t.Fatalf("expected no skipped files, got %d", skipped)
	}
	if cleanupErrors != 0 {
		t.Fatalf("expected no cleanup errors, got %d", cleanupErrors)
	}
	if FileExists(videoPath) {
		t.Fatal("expected standalone video to be deleted")
	}
}

func TestRunMotionPhotoPassContinuesAfterPairFailures(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "motionphoto.args")
	appendLog := filepath.Join(t.TempDir(), "motionphoto.log")
	firstImage := filepath.Join(t.TempDir(), "PXL_0001.jpg")
	firstVideo := filepath.Join(t.TempDir(), "PXL_0001.mp4")
	secondImage := filepath.Join(t.TempDir(), "PXL_0002.jpg")
	secondVideo := filepath.Join(t.TempDir(), "PXL_0002.mp4")

	for path, body := range map[string]string{
		firstImage:  "image-1",
		firstVideo:  "video-1",
		secondImage: "image-2",
		secondVideo: "video-2",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	withFakeMotionPhotoTool(t, map[string]string{
		"FAKE_MOTIONPHOTO_ARGS_FILE": argsFile,
		"FAKE_MOTIONPHOTO_APPEND_TO": appendLog,
		"FAKE_MOTIONPHOTO_EXIT_CODE": "1",
		"FAKE_MOTIONPHOTO_STDOUT":    "partial",
	})

	result := RunMotionPhotoPass([]motionPhotoCleanupTarget{
		{videoOutputPath: firstVideo, imageOutputPath: firstImage},
		{videoOutputPath: secondVideo, imageOutputPath: secondImage},
	}, ProcessOptions{CreateMotionPhotos: true})
	if result.Status != MotionPhotoPassStatusFailed {
		t.Fatalf("expected failed status, got %+v", result)
	}

	logBody := readFileString(t, appendLog)
	if strings.Count(strings.TrimSpace(logBody), "embedded") != 2 {
		t.Fatalf("expected both pairs to be attempted, got log %q", logBody)
	}

	args := readFileString(t, argsFile)
	requireContainsArg(t, args, "--input-image", firstImage)
	requireContainsArg(t, args, "--input-image", secondImage)
}
