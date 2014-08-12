package file

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/mattes/migrate/migrate/direction"
	"go/token"
	"io/ioutil"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var filenameRegex = "^([0-9]+)_(.*)\\.(up|down)\\.%s$"

func FilenameRegex(filenameExtension string) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(filenameRegex, filenameExtension))
}

type File struct {
	Path      string
	FileName  string
	Version   uint64
	Name      string
	Content   []byte
	Direction direction.Direction
}

type Files []File

type MigrationFile struct {
	Version  uint64
	UpFile   *File
	DownFile *File
}

type MigrationFiles []MigrationFile

// Read reads the file contents
func (f *File) ReadContent() error {
	content, err := ioutil.ReadFile(path.Join(f.Path, f.FileName))
	if err != nil {
		return err
	}
	f.Content = content
	return nil
}

// ToFirstFrom fetches all (down) migration files including the migration file
// of the current version to the very first migration file.
func (mf *MigrationFiles) ToFirstFrom(version uint64) (Files, error) {
	sort.Sort(sort.Reverse(mf))
	files := make(Files, 0)
	for _, migrationFile := range *mf {
		if migrationFile.Version <= version && migrationFile.DownFile != nil {
			files = append(files, *migrationFile.DownFile)
		}
	}
	return files, nil
}

// ToLastFrom fetches all (up) migration files to the most recent migration file.
// The migration file of the current version is not included.
func (mf *MigrationFiles) ToLastFrom(version uint64) (Files, error) {
	sort.Sort(mf)
	files := make(Files, 0)
	for _, migrationFile := range *mf {
		if migrationFile.Version > version && migrationFile.UpFile != nil {
			files = append(files, *migrationFile.UpFile)
		}
	}
	return files, nil
}

// From travels relatively through migration files.
// +1 will fetch the next up migration file
// +2 will fetch the next two up migration files
// -1 will fetch the the current down migration file
// -2 will fetch the current down and the next down migration file
func (mf *MigrationFiles) From(version uint64, relativeN int) (Files, error) {
	var d direction.Direction
	if relativeN > 0 {
		d = direction.Up
	} else if relativeN < 0 {
		d = direction.Down
	} else { // relativeN == 0
		return nil, nil
	}

	if d == direction.Down {
		sort.Sort(sort.Reverse(mf))
	} else {
		sort.Sort(mf)
	}

	files := make(Files, 0)

	counter := relativeN
	if relativeN < 0 {
		counter = relativeN * -1
	}

	for _, migrationFile := range *mf {
		if counter > 0 {

			if d == direction.Up && migrationFile.Version > version && migrationFile.UpFile != nil {
				files = append(files, *migrationFile.UpFile)
				counter -= 1
			} else if d == direction.Down && migrationFile.Version <= version && migrationFile.DownFile != nil {
				files = append(files, *migrationFile.DownFile)
				counter -= 1
			}
		} else {
			break
		}
	}
	return files, nil
}

// readMigrationFiles reads all migration files from a given path
func ReadMigrationFiles(path string, filenameRegex *regexp.Regexp) (files MigrationFiles, err error) {
	// find all migration files in path
	ioFiles, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}
	type tmpFile struct {
		version  uint64
		name     string
		filename string
		d        direction.Direction
	}
	tmpFiles := make([]*tmpFile, 0)
	for _, file := range ioFiles {
		version, name, d, err := parseFilenameSchema(file.Name(), filenameRegex)
		if err == nil {
			tmpFiles = append(tmpFiles, &tmpFile{version, name, file.Name(), d})
		}
	}

	// put tmpFiles into MigrationFile struct
	parsedVersions := make(map[uint64]bool)
	newFiles := make(MigrationFiles, 0)
	for _, file := range tmpFiles {
		if _, ok := parsedVersions[file.version]; !ok {
			migrationFile := MigrationFile{
				Version: file.version,
			}

			var lookFordirection direction.Direction
			switch file.d {
			case direction.Up:
				migrationFile.UpFile = &File{
					Path:      path,
					FileName:  file.filename,
					Version:   file.version,
					Name:      file.name,
					Content:   nil,
					Direction: direction.Up,
				}
				lookFordirection = direction.Down
			case direction.Down:
				migrationFile.DownFile = &File{
					Path:      path,
					FileName:  file.filename,
					Version:   file.version,
					Name:      file.name,
					Content:   nil,
					Direction: direction.Down,
				}
				lookFordirection = direction.Up
			default:
				return nil, errors.New("Unsupported direction.Direction Type")
			}

			for _, file2 := range tmpFiles {
				if file2.version == file.version && file2.d == lookFordirection {
					switch lookFordirection {
					case direction.Up:
						migrationFile.UpFile = &File{
							Path:      path,
							FileName:  file2.filename,
							Version:   file.version,
							Name:      file2.name,
							Content:   nil,
							Direction: direction.Up,
						}
					case direction.Down:
						migrationFile.DownFile = &File{
							Path:      path,
							FileName:  file2.filename,
							Version:   file.version,
							Name:      file2.name,
							Content:   nil,
							Direction: direction.Down,
						}
					}
					break
				}
			}

			newFiles = append(newFiles, migrationFile)
			parsedVersions[file.version] = true
		}
	}

	sort.Sort(newFiles)
	return newFiles, nil
}

// parseFilenameSchema parses the filename and returns
// version, name, d (up|down)
// the schema looks like 000_name.(up|down).extension
func parseFilenameSchema(filename string, filenameRegex *regexp.Regexp) (version uint64, name string, d direction.Direction, err error) {
	matches := filenameRegex.FindStringSubmatch(filename)
	if len(matches) != 4 {
		return 0, "", 0, errors.New("Unable to parse filename schema")
	}

	version, err = strconv.ParseUint(matches[1], 10, 0)
	if err != nil {
		return 0, "", 0, errors.New(fmt.Sprintf("Unable to parse version '%v' in filename schema", matches[0]))
	}

	if matches[3] == "up" {
		d = direction.Up
	} else if matches[3] == "down" {
		d = direction.Down
	} else {
		return 0, "", 0, errors.New(fmt.Sprintf("Unable to parse up|down '%v' in filename schema", matches[3]))
	}

	return version, matches[2], d, nil
}

// Len is the number of elements in the collection.
// Required by Sort Interface{}
func (mf MigrationFiles) Len() int {
	return len(mf)
}

// Less reports whether the element with
// index i should sort before the element with index j.
// Required by Sort Interface{}
func (mf MigrationFiles) Less(i, j int) bool {
	return mf[i].Version < mf[j].Version
}

// Swap swaps the elements with indexes i and j.
// Required by Sort Interface{}
func (mf MigrationFiles) Swap(i, j int) {
	mf[i], mf[j] = mf[j], mf[i]
}

// LineColumnFromOffset reads data and returns line and column integer
// for a given offset.
// TODO is there a better way?
func LineColumnFromOffset(data []byte, offset int) (line, column int) {
	fs := token.NewFileSet()
	tf := fs.AddFile("", fs.Base(), len(data))
	tf.SetLinesForContent(data)
	pos := tf.Position(tf.Pos(offset))
	return pos.Line, pos.Column
}

// LinesBeforeAndAfter reads n lines before and after a given line.
// Set lineNumbers to true, to prepend line numbers.
// TODO Trim empty lines at the beginning and at the end
// TODO Trim offset whitespace at the beginning of each line, so that indentation is preserved
func LinesBeforeAndAfter(data []byte, line, before, after int, lineNumbers bool) []byte {
	startLine := line - before
	endLine := line + after
	lines := bytes.SplitN(data, []byte("\n"), endLine+1)

	if startLine < 0 {
		startLine = 0
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	selectLines := lines[startLine:endLine]
	newLines := make([][]byte, 0)
	lineCounter := startLine + 1
	lineNumberDigits := len(strconv.Itoa(len(selectLines)))
	for _, l := range selectLines {
		lineCounterStr := strconv.Itoa(lineCounter)
		if len(lineCounterStr)%lineNumberDigits != 0 {
			lineCounterStr = strings.Repeat(" ", lineNumberDigits-len(lineCounterStr)%lineNumberDigits) + lineCounterStr
		}

		lNew := l
		if lineNumbers {
			lNew = append([]byte(lineCounterStr+": "), lNew...)
		}
		newLines = append(newLines, lNew)
		lineCounter += 1
	}

	return bytes.Join(newLines, []byte("\n"))
}
