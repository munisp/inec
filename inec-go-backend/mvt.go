package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
)

type mvtFeatureData struct {
	id    int
	px    int
	py    int
	props map[string]interface{}
}

func encodeMVTTile(rows *sql.Rows, z, x, y int, lonMin, latMin, lonMax, latMax float64) []byte {
	if rows == nil {
		return encodeMVTEmpty()
	}
	defer rows.Close()

	extent := 4096
	var features []mvtFeatureData

	for rows.Next() {
		var code, name, status string
		var lat, lon float64
		var submittedAt sql.NullString
		var submittedTs sql.NullInt64
		if err := rows.Scan(&code, &name, &lat, &lon, &status, &submittedAt, &submittedTs); err != nil {
			continue
		}

		px := int(math.Round((lon - lonMin) / (lonMax - lonMin) * float64(extent)))
		py := int(math.Round((1.0 - (lat-latMin)/(latMax-latMin)) * float64(extent)))
		if px < 0 {
			px = 0
		}
		if px > extent {
			px = extent
		}
		if py < 0 {
			py = 0
		}
		if py > extent {
			py = extent
		}

		props := map[string]interface{}{
			"code":   code,
			"name":   name,
			"status": status,
		}
		if submittedAt.Valid {
			props["submitted_at"] = submittedAt.String
		}
		if submittedTs.Valid {
			props["submitted_ts"] = submittedTs.Int64
		}

		features = append(features, mvtFeatureData{
			id: len(features), px: px, py: py, props: props,
		})
	}

	return buildMVTLayer("pus", features, extent)
}

func encodeMVTEmpty() []byte {
	return buildMVTLayer("pus", nil, 4096)
}

func buildMVTLayer(name string, features []mvtFeatureData, extent int) []byte {
	var keys []string
	keyIdx := map[string]int{}
	var vals []interface{}
	valIdx := map[string]int{}

	getKeyIdx := func(k string) int {
		if idx, ok := keyIdx[k]; ok {
			return idx
		}
		idx := len(keys)
		keys = append(keys, k)
		keyIdx[k] = idx
		return idx
	}

	valKey := func(v interface{}) string {
		return fmt.Sprintf("%T:%v", v, v)
	}

	getValIdx := func(v interface{}) int {
		vk := valKey(v)
		if idx, ok := valIdx[vk]; ok {
			return idx
		}
		idx := len(vals)
		vals = append(vals, v)
		valIdx[vk] = idx
		return idx
	}

	var encodedFeatures [][]byte
	for _, f := range features {
		var tags []uint32
		for k, v := range f.props {
			tags = append(tags, uint32(getKeyIdx(k)), uint32(getValIdx(v)))
		}
		encodedFeatures = append(encodedFeatures, encodeMVTFeature(f.id, f.px, f.py, tags))
	}

	var layer []byte

	layer = appendVarint(layer, (15<<3)|0)
	layer = appendVarint(layer, 2)

	nameBytes := []byte(name)
	layer = appendVarint(layer, (1<<3)|2)
	layer = appendVarint(layer, uint64(len(nameBytes)))
	layer = append(layer, nameBytes...)

	layer = appendVarint(layer, (5<<3)|0)
	layer = appendVarint(layer, uint64(extent))

	for _, k := range keys {
		kb := []byte(k)
		layer = appendVarint(layer, (3<<3)|2)
		layer = appendVarint(layer, uint64(len(kb)))
		layer = append(layer, kb...)
	}

	for _, v := range vals {
		valMsg := encodeMVTValue(v)
		layer = appendVarint(layer, (4<<3)|2)
		layer = appendVarint(layer, uint64(len(valMsg)))
		layer = append(layer, valMsg...)
	}

	for _, f := range encodedFeatures {
		layer = appendVarint(layer, (2<<3)|2)
		layer = appendVarint(layer, uint64(len(f)))
		layer = append(layer, f...)
	}

	var tile []byte
	tile = appendVarint(tile, (3<<3)|2)
	tile = appendVarint(tile, uint64(len(layer)))
	tile = append(tile, layer...)
	return tile
}

func encodeMVTFeature(id, px, py int, tags []uint32) []byte {
	var buf []byte

	buf = appendVarint(buf, (1<<3)|0)
	buf = appendVarint(buf, uint64(id))

	if len(tags) > 0 {
		var tagBuf []byte
		for _, t := range tags {
			tagBuf = appendVarint(tagBuf, uint64(t))
		}
		buf = appendVarint(buf, (2<<3)|2)
		buf = appendVarint(buf, uint64(len(tagBuf)))
		buf = append(buf, tagBuf...)
	}

	buf = appendVarint(buf, (3<<3)|0)
	buf = appendVarint(buf, 1)

	geom := encodePointGeom(px, py)
	buf = appendVarint(buf, (4<<3)|2)
	buf = appendVarint(buf, uint64(len(geom)))
	buf = append(buf, geom...)

	return buf
}

func encodeMVTValue(v interface{}) []byte {
	var buf []byte
	switch val := v.(type) {
	case string:
		b := []byte(val)
		buf = appendVarint(buf, (1<<3)|2)
		buf = appendVarint(buf, uint64(len(b)))
		buf = append(buf, b...)
	case int64:
		buf = appendVarint(buf, (6<<3)|0)
		buf = appendVarint(buf, zigzag(int(val)))
	case int:
		buf = appendVarint(buf, (6<<3)|0)
		buf = appendVarint(buf, zigzag(val))
	case float64:
		buf = appendVarint(buf, (3<<3)|1)
		var tmp [8]byte
		binary.LittleEndian.PutUint64(tmp[:], math.Float64bits(val))
		buf = append(buf, tmp[:]...)
	case bool:
		buf = appendVarint(buf, (7<<3)|0)
		if val {
			buf = appendVarint(buf, 1)
		} else {
			buf = appendVarint(buf, 0)
		}
	default:
		s := fmt.Sprintf("%v", val)
		b := []byte(s)
		buf = appendVarint(buf, (1<<3)|2)
		buf = appendVarint(buf, uint64(len(b)))
		buf = append(buf, b...)
	}
	return buf
}

func encodePointGeom(px, py int) []byte {
	var packed []byte
	packed = appendVarint(packed, 0x09)
	packed = appendVarint(packed, zigzag(px))
	packed = appendVarint(packed, zigzag(py))
	return packed
}

func zigzag(n int) uint64 {
	return uint64((n << 1) ^ (n >> 63))
}

func appendVarint(buf []byte, v uint64) []byte {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	return append(buf, tmp[:n]...)
}
