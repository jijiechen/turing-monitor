package proto

// Command IDs spoken by the device. Recovered from the vendor's Windows
// application and confirmed against the turing-smart-screen-python reference
// implementation.
const (
	CmdSync           byte = 10  // handshake / keepalive
	CmdRestart        byte = 11  // reboot the device
	CmdRotate         byte = 13  // display rotation
	CmdBrightness     byte = 14  // backlight, 0-102
	CmdFrameRate      byte = 15  // stream frame rate, fps
	CmdGetH264ChunkSz byte = 17  // negotiate H.264 chunk size
	CmdOpenFile       byte = 38  // open a file on the panel for writing
	CmdWriteFile      byte = 39  // write one chunk of that file
	CmdDeleteFile     byte = 40  // delete a file on the panel
	CmdPlayFile       byte = 98  // play a file stored on the panel
	CmdPlayFileAlt    byte = 110 // alternate playback request
	CmdPlayImageFile  byte = 113 // play an image file stored on the panel
	CmdStorageInfo    byte = 100 // SD card usage
	CmdUploadJPEG     byte = 101 // still image, JPEG payload
	CmdUploadPNG      byte = 102 // still image, PNG payload
	CmdPlayH264Chunk  byte = 121 // one chunk of an H.264 stream
	CmdStreamStatus   byte = 122 // playback queue depth
	CmdStopStream     byte = 123 // end playback
	CmdSaveSettings   byte = 125 // persist settings to device storage
)

// Commands sent as a bare header (no payload byte) to put the panel into
// streaming mode. Without this prelude the panel accepts H.264 chunks but keeps
// displaying whatever was on screen before -- it never switches to the stream.
//
// The names are inferred from their position in the vendor application's
// command sequence and from which method emits them; the protocol has no
// published names.
const (
	CmdStopVideo  byte = 111 // halts playback already in progress
	CmdResetVideo byte = 112 // resets the video pipeline
	CmdStreamFlag byte = 41  // part of the streaming prelude
)

// CommandName maps a command ID to a human-readable name, for logging and
// diagnostics.
func CommandName(cmd byte) string {
	switch cmd {
	case CmdSync:
		return "sync"
	case CmdRestart:
		return "restart"
	case CmdRotate:
		return "rotate"
	case CmdBrightness:
		return "brightness"
	case CmdFrameRate:
		return "frame-rate"
	case CmdGetH264ChunkSz:
		return "get-h264-chunk-size"
	case CmdOpenFile:
		return "open-file"
	case CmdWriteFile:
		return "write-file"
	case CmdDeleteFile:
		return "delete-file"
	case CmdPlayFile:
		return "play-file"
	case CmdPlayFileAlt:
		return "play-file-alt"
	case CmdPlayImageFile:
		return "play-image-file"
	case CmdStorageInfo:
		return "storage-info"
	case CmdUploadJPEG:
		return "upload-jpeg"
	case CmdUploadPNG:
		return "upload-png"
	case CmdPlayH264Chunk:
		return "play-h264-chunk"
	case CmdStreamStatus:
		return "stream-status"
	case CmdStopStream:
		return "stop-stream"
	case CmdStopVideo:
		return "stop-video"
	case CmdResetVideo:
		return "reset-video"
	case CmdStreamFlag:
		return "stream-flag"
	case CmdSaveSettings:
		return "save-settings"
	default:
		return "unknown"
	}
}
