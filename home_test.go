package metha

import (
	"os"
	"runtime"
	"testing"
)

// Regular tests that run on all platforms
func TestUserHomeDir(t *testing.T) {
	homeDir := UserHomeDir()
	if homeDir == "" {
		t.Error("expected non-empty home directory, got empty string")
	}
}

// TestUserHomeDirWithEnvMock tests the logic by temporarily changing environment variables
func TestUserHomeDirWithEnvMock(t *testing.T) {
	if runtime.GOOS == "windows" {
		testWindowsLogic(t)
	} else {
		testWindowsLogic(t)
		testUnixLogic(t)
	}
}

func testWindowsLogic(t *testing.T) {
	// Test with HOMEDRIVE + HOMEPATH (common Windows scenario)
	t.Run("Windows_HOMEDRIVE_HOMEPATH", func(t *testing.T) {
		// Note: This test only fully validates the Windows path when running on Windows
		// On non-Windows platforms, we're just ensuring the function doesn't crash
		var (
			originalHomeDrive   = os.Getenv("HOMEDRIVE")
			originalHomePath    = os.Getenv("HOMEPATH")
			originalUserProfile = os.Getenv("USERPROFILE")
			originalHome        = os.Getenv("HOME")
		)
		defer func() {
			os.Setenv("HOMEDRIVE", originalHomeDrive)
			os.Setenv("HOMEPATH", originalHomePath)
			os.Setenv("USERPROFILE", originalUserProfile)
			os.Setenv("HOME", originalHome)
		}()
		os.Setenv("HOMEDRIVE", "C:")
		os.Setenv("HOMEPATH", "\\Users\\TestUser")
		os.Setenv("USERPROFILE", "")
		os.Setenv("HOME", "")

		result := UserHomeDir()
		// On Windows, the result should be "C:\\Users\\TestUser"
		// On non-Windows, it will return the $HOME value which should be empty
		if runtime.GOOS == "windows" {
			expected := "C:\\Users\\TestUser"
			if result != expected {
				t.Errorf("Expected %q, got %q", expected, result)
			}
		} else {
			// On non-Windows systems, the function will return os.Getenv("HOME") which we set to ""
			if result != "" {
				t.Errorf("on non-Windows systems, expected empty string, got %q", result)
			}
		}
	})

	// Test with USERPROFILE (fallback Windows scenario)
	t.Run("Windows_USERPROFILE", func(t *testing.T) {
		var (
			originalHomeDrive   = os.Getenv("HOMEDRIVE")
			originalHomePath    = os.Getenv("HOMEPATH")
			originalUserProfile = os.Getenv("USERPROFILE")
			originalHome        = os.Getenv("HOME")
		)
		defer func() {
			os.Setenv("HOMEDRIVE", originalHomeDrive)
			os.Setenv("HOMEPATH", originalHomePath)
			os.Setenv("USERPROFILE", originalUserProfile)
			os.Setenv("HOME", originalHome)
		}()
		os.Setenv("HOMEDRIVE", "")
		os.Setenv("HOMEPATH", "")
		os.Setenv("USERPROFILE", "C:\\Users\\TestUser")
		os.Setenv("HOME", "")

		result := UserHomeDir()
		if runtime.GOOS == "windows" {
			expected := "C:\\Users\\TestUser"
			if result != expected {
				t.Errorf("expected %q, got %q", expected, result)
			}
		} else {
			// On non-Windows systems, the function will return os.Getenv("HOME") which we set to ""
			if result != "" {
				t.Errorf("on non-Windows systems, expected empty string, got %q", result)
			}
		}
	})

	// Test with empty Windows environment variables
	t.Run("Windows_Empty", func(t *testing.T) {
		var (
			originalHomeDrive   = os.Getenv("HOMEDRIVE")
			originalHomePath    = os.Getenv("HOMEPATH")
			originalUserProfile = os.Getenv("USERPROFILE")
			originalHome        = os.Getenv("HOME")
		)
		defer func() {
			os.Setenv("HOMEDRIVE", originalHomeDrive)
			os.Setenv("HOMEPATH", originalHomePath)
			os.Setenv("USERPROFILE", originalUserProfile)
			os.Setenv("HOME", originalHome)
		}()
		os.Setenv("HOMEDRIVE", "")
		os.Setenv("HOMEPATH", "")
		os.Setenv("USERPROFILE", "")
		os.Setenv("HOME", "")

		result := UserHomeDir()
		// On Windows, should return empty string when all variables are empty
		// On non-Windows, should return empty string because HOME is empty
		if result != "" {
			t.Errorf("expected empty string when all environment variables are empty, got %q", result)
		}
	})
}

func testUnixLogic(t *testing.T) {
	// Test Unix logic with HOME set
	t.Run("Unix_HOME_Set", func(t *testing.T) {
		originalHome := os.Getenv("HOME")

		defer func() {
			os.Setenv("HOME", originalHome)
		}()

		os.Setenv("HOME", "/home/testuser")

		result := UserHomeDir()
		expected := "/home/testuser"
		if result != expected {
			t.Errorf("Expected %q, got %q", expected, result)
		}
	})

	// Test Unix logic with HOME unset
	t.Run("Unix_HOME_Empty", func(t *testing.T) {
		originalHome := os.Getenv("HOME")

		defer func() {
			os.Setenv("HOME", originalHome)
		}()
		os.Setenv("HOME", "")
		var (
			result   = UserHomeDir()
			expected = "" // Should be empty when HOME is empty
		)
		if result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})
}

// Windows-specific tests
func TestUserHomeDirWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		homeDir := UserHomeDir()
		if homeDir == "" {
			t.Error("expected non-empty home directory on Windows, got empty string")
		}
	}
}

// Unix-specific tests
func TestUserHomeDirUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Skip this test on Windows as it tests Unix-specific behavior
		t.Skip("skipping Unix-specific test on Windows")
	}
	homeDir := UserHomeDir()
	if homeDir == "" {
		t.Error("expected non-empty home directory on Unix-like systems, got empty string")
	}
	// Verify that the home directory starts with a slash on Unix-like systems
	if homeDir[0] != '/' {
		t.Errorf("expected home directory to start with '/', got %q", homeDir)
	}
}
