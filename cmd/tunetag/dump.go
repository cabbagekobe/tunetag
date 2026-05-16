package main

import (
	"fmt"
	"strings"

	"github.com/cabbagekobe/tunetag"
	"github.com/cabbagekobe/tunetag/aac"
	"github.com/cabbagekobe/tunetag/aiff"
	"github.com/cabbagekobe/tunetag/ape"
	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
	"github.com/cabbagekobe/tunetag/ogg"
	"github.com/cabbagekobe/tunetag/wav"
)

// cmdDump prints every parsed field, unlike `print` which shows only
// the common Tag interface subset. Useful for verifying that the
// library actually preserves the entire metadata region.
func cmdDump(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("dump: exactly one file argument required")
	}
	path := args[0]
	format, err := detect(path)
	if err != nil {
		return err
	}
	fmt.Printf("File:   %s\n", path)
	fmt.Printf("Format: %s\n", format)
	fmt.Println(strings.Repeat("─", 60))
	switch format {
	case tunetag.FormatID3v2:
		return dumpID3v2(path)
	case tunetag.FormatID3v1:
		return dumpID3v1(path)
	case tunetag.FormatFLAC:
		return dumpFLAC(path)
	case tunetag.FormatMP4:
		return dumpMP4(path)
	case tunetag.FormatWAV:
		return dumpWAV(path)
	case tunetag.FormatAIFF:
		return dumpAIFF(path)
	case tunetag.FormatOgg:
		return dumpOgg(path)
	case tunetag.FormatAPE:
		return dumpAPE(path)
	case tunetag.FormatAAC:
		return dumpAAC(path)
	}
	return fmt.Errorf("dump: unsupported format %s", format)
}

// --- ID3v2 ---------------------------------------------------

func dumpID3v2(path string) error {
	t, err := id3v2.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("Tag header: Version=%s Flags=0x%02X Padding=%d FrameCount=%d\n\n",
		t.Version, byte(t.Flags), t.Padding, len(t.Frames))
	for i, f := range t.Frames {
		fmt.Printf("Frame[%2d] %s  (%T)\n", i, f.ID(), f)
		switch v := f.(type) {
		case *id3v2.TextFrame:
			fmt.Printf("   Encoding: %s\n", v.Encoding)
			for j, s := range v.Text {
				fmt.Printf("   Text[%d]:  %q\n", j, s)
			}
		case *id3v2.UserTextFrame:
			fmt.Printf("   Encoding: %s  Desc=%q  Value=%q\n",
				v.Encoding, v.Description, v.Value)
		case *id3v2.CommentFrame:
			fmt.Printf("   Encoding: %s  Lang=%q  Desc=%q  Text=%q\n",
				v.Encoding, v.Language, v.Description, v.Text)
		case *id3v2.UnsynchronisedLyricsFrame:
			fmt.Printf("   Encoding: %s  Lang=%q  Desc=%q  Text=%q\n",
				v.Encoding, v.Language, v.Description, v.Text)
		case *id3v2.PictureFrame:
			fmt.Printf("   Encoding: %s  MIME=%q  Type=%d  Desc=%q  Data=%d bytes\n",
				v.Encoding, v.MIME, v.PictureType, v.Description, len(v.Data))
		case *id3v2.URLFrame:
			fmt.Printf("   URL: %q\n", v.URL)
		case *id3v2.UserURLFrame:
			fmt.Printf("   Encoding: %s  Desc=%q  URL=%q\n",
				v.Encoding, v.Description, v.URL)
		case *id3v2.UFIDFrame:
			fmt.Printf("   Owner=%q  Identifier=% X (%d bytes total)\n",
				v.Owner, head(v.Identifier, 32), len(v.Identifier))
		case *id3v2.PrivFrame:
			fmt.Printf("   Owner=%q  Data=% X (%d bytes total)\n",
				v.Owner, head(v.Data, 32), len(v.Data))
		case *id3v2.GenericFrame:
			fmt.Printf("   StatusFlags=0x%02X FormatFlags=0x%02X  Body=% X (%d bytes total)\n",
				v.StatusFlags, v.FormatFlags, head(v.Body, 32), len(v.Body))
		}
	}
	return nil
}

// --- ID3v1 ---------------------------------------------------

func dumpID3v1(path string) error {
	t, err := id3v1.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("Title:   %q\n", t.Title)
	fmt.Printf("Artist:  %q\n", t.Artist)
	fmt.Printf("Album:   %q\n", t.Album)
	fmt.Printf("Year:    %q\n", t.Year)
	fmt.Printf("Comment: %q\n", t.Comment)
	fmt.Printf("Track:   %d\n", t.Track)
	fmt.Printf("Genre:   %d (%s)\n", t.Genre, t.GenreName())
	return nil
}

// --- FLAC ----------------------------------------------------

func dumpFLAC(path string) error {
	f, err := flac.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("Blocks: %d\n\n", len(f.Blocks))
	for i, b := range f.Blocks {
		switch v := b.(type) {
		case *flac.RawBlock:
			name := flacBlockName(v.BlockType)
			fmt.Printf("Block[%d] type=%d (%s) raw=%d bytes\n",
				i, v.BlockType, name, len(v.Body))
			if v.BlockType == flac.BlockStreamInfo && len(v.Body) >= 18 {
				// STREAMINFO begins with min/max block & frame sizes.
				fmt.Printf("   first 18 bytes: % X\n", v.Body[:18])
			}
		case *flac.VorbisComment:
			fmt.Printf("Block[%d] VORBIS_COMMENT  vendor=%q  comments=%d\n",
				i, v.Vendor, len(v.Comments))
			for j, c := range v.Comments {
				fmt.Printf("   [%d] %s\n", j, c)
			}
		case *flac.Picture:
			fmt.Printf("Block[%d] PICTURE  type=%d  mime=%q  desc=%q  %dx%d  depth=%d  data=%d bytes\n",
				i, v.PictureType, v.MIME, v.Description,
				v.Width, v.Height, v.Depth, len(v.Data))
		case *flac.PaddingBlock:
			fmt.Printf("Block[%d] PADDING  size=%d bytes\n", i, v.Size)
		default:
			fmt.Printf("Block[%d] %T\n", i, v)
		}
	}
	return nil
}

func flacBlockName(t uint8) string {
	switch t {
	case flac.BlockStreamInfo:
		return "STREAMINFO"
	case flac.BlockPadding:
		return "PADDING"
	case flac.BlockApplication:
		return "APPLICATION"
	case flac.BlockSeekTable:
		return "SEEKTABLE"
	case flac.BlockVorbisComment:
		return "VORBIS_COMMENT"
	case flac.BlockCueSheet:
		return "CUESHEET"
	case flac.BlockPicture:
		return "PICTURE"
	}
	return "unknown"
}

// --- MP4 -----------------------------------------------------

func dumpMP4(path string) error {
	f, err := mp4.Read(path)
	if err != nil {
		return err
	}
	fmt.Printf("ilst items: %d\n\n", len(f.Tag.Items))
	for i, it := range f.Tag.Items {
		key := prettyKey(it.Key)
		if it.Key == "----" {
			fmt.Printf("Item[%2d] %s (freeform: %s/%s)\n", i, key, it.MeanDomain, it.Name)
		} else {
			fmt.Printf("Item[%2d] %s\n", i, key)
		}
		for j, d := range it.Data {
			switch d.TypeCode {
			case mp4.DataTypeUTF8:
				fmt.Printf("   Data[%d] UTF-8   %q\n", j, string(d.Payload))
			case mp4.DataTypeBEInt:
				v, _ := d.Int()
				fmt.Printf("   Data[%d] int     %d  (%d bytes)\n", j, v, len(d.Payload))
			case mp4.DataTypeJPEG:
				fmt.Printf("   Data[%d] JPEG    %d bytes\n", j, len(d.Payload))
			case mp4.DataTypePNG:
				fmt.Printf("   Data[%d] PNG     %d bytes\n", j, len(d.Payload))
			case mp4.DataTypeBinary:
				if it.Key == "trkn" || it.Key == "disk" {
					n, total, _ := d.TrackNumber()
					fmt.Printf("   Data[%d] binary  %d/%d  (% X)\n",
						j, n, total, d.Payload)
				} else {
					fmt.Printf("   Data[%d] binary  %d bytes (% X)\n",
						j, len(d.Payload), head(d.Payload, 16))
				}
			default:
				fmt.Printf("   Data[%d] type=%d %d bytes\n",
					j, d.TypeCode, len(d.Payload))
			}
		}
	}
	return nil
}

// prettyKey formats an iTunes 4-byte key for display: 0xA9 becomes ©.
func prettyKey(k string) string {
	if len(k) == 4 && k[0] == 0xA9 {
		return "©" + k[1:]
	}
	return k
}

func head(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}

// --- AIFF ----------------------------------------------------

func dumpAIFF(path string) error {
	f, err := aiff.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("Form type: %s\n", f.FormType)
	fmt.Printf("Text chunks: %d\n", len(f.Text))
	for k, v := range f.Text {
		fmt.Printf("   %s = %q\n", k, v)
	}
	fmt.Printf("Annotations: %d\n", len(f.Annotations))
	for i, a := range f.Annotations {
		fmt.Printf("   [%d] %q\n", i, a)
	}
	if f.ID3 == nil {
		fmt.Println("ID3 chunk: (none)")
		return nil
	}
	fmt.Printf("ID3 chunk: Version=%s FrameCount=%d\n", f.ID3.Version, len(f.ID3.Frames))
	for _, fr := range f.ID3.Frames {
		fmt.Printf("   %s\n", fr.ID())
	}
	return nil
}

// --- Ogg -----------------------------------------------------

func dumpOgg(path string) error {
	f, err := ogg.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("Codec:  %s\n", f.Codec)
	fmt.Printf("Vendor: %q\n", f.Vendor)
	if f.Comments != nil {
		fmt.Printf("Comments: %d\n", len(f.Comments.Comments))
		for _, c := range f.Comments.Comments {
			fmt.Printf("   %s\n", c)
		}
	}
	return nil
}

// --- APE -----------------------------------------------------

func dumpAPE(path string) error {
	t, err := ape.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("APEv2 (has-header=%t): %d items\n", t.HasHeader, len(t.Items))
	for _, it := range t.Items {
		switch it.Type {
		case ape.ItemBinary:
			fmt.Printf("   [bin] %s  %d bytes\n", it.Key, len(it.Value))
		case ape.ItemURL:
			fmt.Printf("   [url] %s = %q\n", it.Key, it.String())
		default:
			fmt.Printf("   %s = %q\n", it.Key, it.String())
		}
	}
	return nil
}

// --- AAC -----------------------------------------------------

func dumpAAC(path string) error {
	f, err := aac.ReadFile(path)
	if err != nil {
		return err
	}
	if f.V2 == nil {
		fmt.Println("ID3v2: (none)")
	} else {
		fmt.Printf("ID3v2: Version=%s FrameCount=%d\n", f.V2.Version, len(f.V2.Frames))
		for _, fr := range f.V2.Frames {
			fmt.Printf("   %s\n", fr.ID())
		}
	}
	if f.V1 == nil {
		fmt.Println("ID3v1: (none)")
		return nil
	}
	fmt.Printf("ID3v1: Title=%q Artist=%q Album=%q Year=%q\n",
		f.V1.Title, f.V1.Artist, f.V1.Album, f.V1.Year)
	return nil
}

// --- WAV -----------------------------------------------------

func dumpWAV(path string) error {
	f, err := wav.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("LIST/INFO entries: %d\n", len(f.Info))
	for _, it := range f.Info {
		fmt.Printf("   %s = %q\n", it.ID, it.Value)
	}
	if f.ID3 == nil {
		fmt.Println("id3 chunk: (none)")
		return nil
	}
	fmt.Printf("id3 chunk: Version=%s FrameCount=%d\n", f.ID3.Version, len(f.ID3.Frames))
	for _, fr := range f.ID3.Frames {
		fmt.Printf("   %s\n", fr.ID())
	}
	return nil
}
