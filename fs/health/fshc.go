// Package health is a basic mountpath health monitor.
/*
 * Copyright (c) 2018-2024, NVIDIA CORPORATION. All rights reserved.
 */
package health

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/cmn/cos"
	"github.com/NVIDIA/aistore/cmn/nlog"
	"github.com/NVIDIA/aistore/fs"
)

// When triggered (via `OnErr`), FSHC runs assorted tests to check health of the
// associated mountpath.
// If:
// - the mountpath appears to be unavailable, or
// - configured error limit is exceeded
// the mountpath is disabled - effectively, removed from the operation henceforth.

// TODO -- FIXME: revisit all tunables

const (
	ival = 4 * time.Minute
)

const (
	fshcFileSize    = 10 * cos.MiB // size of temporary file which will test writing and reading the mountpath
	fshcMaxFileList = 100          // maximum number of files to read by Readdir
)

type (
	disabler interface {
		DisableMpath(mi *fs.Mountpath) error // impl. ais/tgtfshc.go
	}
	FSHC struct {
		t disabler
	}
)

func NewFSHC(t disabler) (f *FSHC) { return &FSHC{t: t} }

func (f *FSHC) run(mi *fs.Mountpath, fqn string) {
	var (
		serr         string
		rerrs, werrs int
		cfg          = cmn.GCO.Get().FSHC
		dup          = fs.Mountpath{Path: mi.Path}
	)
	// 1. fstat
	err := cos.Stat(mi.Path)
	if err != nil {
		nlog.Errorln("fstat err #1:", err)
		time.Sleep(time.Second)
		if _, err := os.Stat(mi.Path); err != nil {
			nlog.Errorln("critical fstat err #2:", err)
			goto disable
		}
	}

	// 2. resolve FS
	err = dup.ResolveFS()
	if err == nil {
		if !dup.FS.Equal(mi.FS) {
			err = fmt.Errorf("%s: detected filesystem change (%s => %s) at runtime", mi, mi.FS.String(), dup.FS.String())
		}
	}
	if err != nil {
		nlog.Errorln(err)
		goto disable
	}

	// 3. refresh disks
	if err = mi.RefreshDisks(); err != nil {
		nlog.Errorln(err)
		goto disable
	}

	// double-check
	if !mi.IsAvail() {
		nlog.Warningln(mi.String(), "is not available, nothing to do")
	}

	// 4. read/write tests
	rerrs, werrs = _rwMpath(mi, fqn, cfg.TestFileCount, fshcFileSize)

	if rerrs == 0 && werrs == 0 {
		nlog.Infoln(mi.String(), "is healthy")
		return
	}
	serr = fmt.Sprintf("(read %d, write %d, err-limit %d, write-size %s)", rerrs, werrs, cfg.ErrorLimit, cos.ToSizeIEC(fshcFileSize, 0))

	if rerrs < cfg.ErrorLimit && werrs < cfg.ErrorLimit {
		nlog.Errorln("Warning: detected errors reading/writing", mi.String(), serr)
		return
	}
	nlog.Errorln("exceeded I/O error limit, proceeding to disable", mi.String(), serr)

disable:
	f._disable(mi)
}

func (f *FSHC) _disable(mi *fs.Mountpath) {
	if err := f.t.DisableMpath(mi); err != nil {
		nlog.Errorf("%s: failed to disable, err: %v", mi, err)
	} else {
		nlog.Infoln(mi.String(), "now disabled")
		mi.SetFlags(fs.FlagDisabledByFSHC)
	}
}

// the core testing function: reads existing and writes temporary files on mountpath
//  1. If the filepath points to existing file, it reads this file
//  2. Reads up to maxReads files selected at random
//  3. Creates up to maxWrites temporary files
//
// The function returns the number of read/write errors, and if the mountpath
//
//	is accessible. When the specified local directory is inaccessible the
//	function returns immediately without any read/write operations
func _rwMpath(mi *fs.Mountpath, fqn string, numFiles, fsize int) (rerrs, werrs int) {
	var numReads int

	// 1. Read the fqn that caused the error, if defined and is a file.
	if fqn != "" {
		nlog.Infoln("1. read failed fqn", fqn)
		if finfo, err := os.Stat(fqn); err == nil && !finfo.IsDir() {
			numReads++
			if err := _read(fqn); err != nil && !os.IsNotExist(err) {
				nlog.Errorln(fqn+":", err)
				if cos.IsIOError(err) {
					rerrs++
				}
			}
		}
	}

	// 2. Read up to numFiles files.
	nlog.Infoln("2. read randomly up to", numFiles, "existing files")
	for numReads < numFiles {
		fqn, err := getRandomFname(mi.Path)
		if err == io.EOF {
			nlog.Warningln(mi.String(), "is suspiciously empty (???)")
			break
		}
		numReads++
		if err != nil {
			if cos.IsIOError(err) {
				rerrs++
			}
			nlog.Errorf("%s: failed to select random (%d, %v)", mi, rerrs, err)
			continue
		}
		if err = _read(fqn); err != nil {
			if cos.IsIOError(err) {
				rerrs++
			}
			nlog.Errorf("%s: failed to read (%s, %d, %v)", mi, fqn, rerrs, err)
		}
	}

	// Create temp dir under the mountpath (under $deleted).
	tmpDir := mi.TempDir("fshc-on-err")
	if err := cos.CreateDir(tmpDir); err != nil {
		if cos.IsIOError(err) {
			werrs++
		}
		nlog.Errorf("%s: failed to create temp dir (%d, %v)", mi, werrs, err)
		return rerrs, werrs
	}

	// 3. Generate and write numFiles files.
	nlog.Infoln("3. write", numFiles, "temp files to", tmpDir)
	for numWrites := 1; numWrites <= numFiles; numWrites++ {
		if err := _write(tmpDir, fsize); err != nil {
			if cos.IsIOError(err) {
				werrs++
			}
			nlog.Errorf("%s: %v (%d)", mi, err, werrs)
		}
	}

	// 4. Remove temp dir
	nlog.Infoln("4. remove", tmpDir)
	if err := os.RemoveAll(tmpDir); err != nil {
		if cos.IsIOError(err) {
			werrs++
		}
		nlog.Errorf("%s: %v (%d)", mi, err, werrs)
	}

	return rerrs, werrs
}

//
// helper methods
//

// Open (O_DIRECT), read, and dicard.
func _read(fqn string) error {
	file, err := fs.DirectOpen(fqn, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, file); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// Write random file under `tmpDir`.
func _write(tmpDir string, fsize int) error {
	fname := filepath.Join(tmpDir, cos.CryptoRandS(10))
	wfh, err := fs.DirectOpen(fname, os.O_RDWR|os.O_CREATE|os.O_TRUNC, cos.PermRWR)
	if err != nil {
		return err
	}

	if err = cos.FloodWriter(wfh, int64(fsize)); err != nil {
		nlog.Errorln("failed to flood-write", fname, err)
		goto cleanup
	}
	if err = wfh.Sync(); err != nil {
		nlog.Errorln("failed to fsync", fname, err)
		goto cleanup
	}

cleanup:
	erc := wfh.Close()
	if erc != nil {
		nlog.Errorln("failed to fclose", fname, erc)
	}
	erd := cos.RemoveFile(fname)
	if erd != nil {
		nlog.Errorln("failed to remove", fname, erd)
	}

	if err == nil && erc == nil && erd == nil {
		return nil
	}
	if err != nil {
		return err
	}
	if erc != nil {
		return erc
	}
	return erd
}

// Look up a random file to read inside `basePath`.
func getRandomFname(basePath string) (string, error) {
	file, err := os.Open(basePath)
	if err != nil {
		return "", err
	}

	files, err := file.ReadDir(fshcMaxFileList)
	if err == nil {
		fmap := make(map[string]os.DirEntry, len(files))
		for _, ff := range files {
			fmap[ff.Name()] = ff
		}

		// look for a non-empty random entry
		for k, info := range fmap {
			// it is a file - return its fqn
			if !info.IsDir() {
				return filepath.Join(basePath, k), nil
			}
			// it is a directory - return a random file from it
			chosen, err := getRandomFname(filepath.Join(basePath, k))
			if err != nil {
				return "", err
			}
			if chosen != "" {
				return chosen, nil
			}
		}
	}
	return "", err
}
