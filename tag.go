package tunetag

// Format identifies the on-disk container of an audio file.
type Format int

const (
	FormatUnknown Format = iota
	FormatID3v1
	FormatID3v2
	FormatFLAC
	FormatMP4
	FormatWAV
	FormatAIFF
	FormatOgg
	FormatAPE
	FormatAAC
	FormatASF
)

func (f Format) String() string {
	switch f {
	case FormatID3v1:
		return "ID3v1"
	case FormatID3v2:
		return "ID3v2"
	case FormatFLAC:
		return "FLAC"
	case FormatMP4:
		return "MP4"
	case FormatWAV:
		return "WAV"
	case FormatAIFF:
		return "AIFF"
	case FormatOgg:
		return "Ogg"
	case FormatAPE:
		return "APEv2"
	case FormatAAC:
		return "AAC"
	case FormatASF:
		return "ASF"
	default:
		return "Unknown"
	}
}

// PictureType is the role of an embedded picture. The 21 values are
// shared between ID3v2 APIC frames and FLAC METADATA_BLOCK_PICTURE.
type PictureType uint8

const (
	PictureOther             PictureType = 0
	PictureFileIcon32        PictureType = 1
	PictureFileIcon          PictureType = 2
	PictureCoverFront        PictureType = 3
	PictureCoverBack         PictureType = 4
	PictureLeafletPage       PictureType = 5
	PictureMedia             PictureType = 6
	PictureLeadArtist        PictureType = 7
	PictureArtist            PictureType = 8
	PictureConductor         PictureType = 9
	PictureBand              PictureType = 10
	PictureComposer          PictureType = 11
	PictureLyricist          PictureType = 12
	PictureRecordingLocation PictureType = 13
	PictureDuringRecording   PictureType = 14
	PictureDuringPerformance PictureType = 15
	PictureMovieScreenshot   PictureType = 16
	PictureBrightColouredFish PictureType = 17
	PictureIllustration      PictureType = 18
	PictureBandLogo          PictureType = 19
	PicturePublisherLogo     PictureType = 20
)

// Picture is an embedded image attached to an audio file.
type Picture struct {
	MIME        string
	Type        PictureType
	Description string
	Data        []byte
}

// Tag is the read-only common interface implemented by every
// format-specific tag type. Setters are intentionally absent — write
// semantics differ enough between formats that a unified setter API
// would be misleading. Use the format subpackages for writes.
type Tag interface {
	Title() string
	Artist() string
	AlbumArtist() string
	Album() string
	Year() int
	TrackNumber() (n, total int)
	DiscNumber() (n, total int)
	Genre() string
	Composer() string
	Comment() string
	Pictures() []Picture
	Format() Format
}
