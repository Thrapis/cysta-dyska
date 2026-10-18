package scan

// hardlinkID builds the dedup key finalize uses to count a hardlinked file
// once. Unix inode numbers are only unique per device, so the device is mixed
// in. Files with a single link get 0 (never deduplicated); that also keeps the
// seen-map small on big volumes.
func hardlinkID(dev, ino, nlink uint64) uint64 {
	if nlink <= 1 || ino == 0 {
		return 0
	}
	id := ino ^ dev*0x9E3779B97F4A7C15
	if id == 0 {
		id = ino
	}
	return id
}
