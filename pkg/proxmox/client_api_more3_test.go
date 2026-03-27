package proxmox

import (
	"context"
	"net/http"
	"testing"
)

func TestClientVMFSInfoParsing(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/node1/qemu/100/agent/get-fsinfo":
			writeJSON(t, w, map[string]interface{}{
				"data": map[string]interface{}{
					"result": []map[string]interface{}{
						{
							"name":        "root",
							"type":        "ext4",
							"mountpoint":  "/",
							"total-bytes": 100,
							"used-bytes":  10,
							"disk": []map[string]interface{}{
								{"dev": "/dev/sda"},
							},
						},
						{
							"name":        "windows",
							"type":        "ntfs",
							"mountpoint":  "C:\\Windows",
							"total-bytes": 200,
							"used-bytes":  20,
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()
	filesystems, err := client.GetVMFSInfo(ctx, "node1", 100)
	if err != nil {
		t.Fatalf("GetVMFSInfo error: %v", err)
	}
	if len(filesystems) != 2 {
		t.Fatalf("expected 2 filesystems, got %d", len(filesystems))
	}
	if filesystems[0].Disk != "/dev/sda" {
		t.Fatalf("expected disk from metadata, got %q", filesystems[0].Disk)
	}
	if filesystems[1].Disk != "C:" {
		t.Fatalf("expected windows drive disk, got %q", filesystems[1].Disk)
	}
}

func TestClientVMFSInfoParsingPrivilegedCapacityFallback(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/node1/qemu/100/agent/get-fsinfo":
			writeJSON(t, w, map[string]interface{}{
				"data": map[string]interface{}{
					"result": []map[string]interface{}{
						{
							"name":                   "windows",
							"type":                   "ntfs",
							"mountpoint":             "C:\\Windows",
							"total-bytes":            0,
							"total-bytes-privileged": 500,
							"used-bytes":             200,
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()
	filesystems, err := client.GetVMFSInfo(ctx, "node1", 100)
	if err != nil {
		t.Fatalf("GetVMFSInfo error: %v", err)
	}
	if len(filesystems) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(filesystems))
	}
	if filesystems[0].TotalBytes != 500 {
		t.Fatalf("expected privileged total-bytes fallback, got %d", filesystems[0].TotalBytes)
	}
	if filesystems[0].Disk != "C:" {
		t.Fatalf("expected windows drive disk, got %q", filesystems[0].Disk)
	}
}

func TestClientVMFSInfoParsingWindowsNameMountpointFallback(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/node1/qemu/100/agent/get-fsinfo":
			writeJSON(t, w, map[string]interface{}{
				"data": map[string]interface{}{
					"result": []map[string]interface{}{
						{
							"name":        "C:\\Windows",
							"type":        "ntfs",
							"mountpoint":  "",
							"total-bytes": 500,
							"used-bytes":  200,
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()
	filesystems, err := client.GetVMFSInfo(ctx, "node1", 100)
	if err != nil {
		t.Fatalf("GetVMFSInfo error: %v", err)
	}
	if len(filesystems) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(filesystems))
	}
	if filesystems[0].Mountpoint != "C:\\Windows" {
		t.Fatalf("expected windows mountpoint fallback, got %q", filesystems[0].Mountpoint)
	}
	if filesystems[0].Disk != "C:" {
		t.Fatalf("expected windows drive disk, got %q", filesystems[0].Disk)
	}
}

func TestClientVMFSInfoParsingWindowsVolumeGUIDMountpointFallback(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/node1/qemu/100/agent/get-fsinfo":
			writeJSON(t, w, map[string]interface{}{
				"data": map[string]interface{}{
					"result": []map[string]interface{}{
						{
							"name":        "C:\\",
							"type":        "ntfs",
							"mountpoint":  "\\\\?\\Volume{1234-5678}\\",
							"total-bytes": 500,
							"used-bytes":  200,
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()
	filesystems, err := client.GetVMFSInfo(ctx, "node1", 100)
	if err != nil {
		t.Fatalf("GetVMFSInfo error: %v", err)
	}
	if len(filesystems) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(filesystems))
	}
	if filesystems[0].Mountpoint != "C:\\" {
		t.Fatalf("expected windows drive-letter mountpoint fallback, got %q", filesystems[0].Mountpoint)
	}
	if filesystems[0].Disk != "C:" {
		t.Fatalf("expected windows drive disk, got %q", filesystems[0].Disk)
	}
}

func TestClientVMFSInfoObjectResult(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/node1/qemu/100/agent/get-fsinfo":
			writeJSON(t, w, map[string]interface{}{
				"data": map[string]interface{}{
					"result": map[string]interface{}{"error": "no info"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()
	filesystems, err := client.GetVMFSInfo(ctx, "node1", 100)
	if err != nil {
		t.Fatalf("GetVMFSInfo error: %v", err)
	}
	if len(filesystems) != 0 {
		t.Fatalf("expected empty filesystem list, got %d", len(filesystems))
	}
}

func TestClientContainerInterfacesError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/node1/lxc/101/interfaces":
			http.Error(w, "boom", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()
	if _, err := client.GetContainerInterfaces(ctx, "node1", 101); err == nil {
		t.Fatal("expected error for non-200 interface response")
	}
}
