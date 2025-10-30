package pbcodec

import "errors"

type AllowRequest struct {
	Key        string `json:"key"`
	Tokens     int64  `json:"tokens"`
	MaxTokens  int64  `json:"max_tokens"`
	RefillRate int64  `json:"refill_rate"`
	Source     string `json:"source"`
}

type AllowResponse struct {
	Allowed         bool   `json:"allowed"`
	RemainingTokens int64  `json:"remaining_tokens"`
	Message         string `json:"message"`
	AllowedHits     int64  `json:"allowed_hits"`
	DeniedHits      int64  `json:"denied_hits"`
	LastAllowedAt   string `json:"last_allowed_at"`
	LastDeniedAt    string `json:"last_denied_at"`
	Algorithm       string `json:"algorithm"`
	StrategyReason  string `json:"strategy_reason"`
}

func MarshalAllowRequest(req *AllowRequest) []byte {
	buf := make([]byte, 0)
	if req.Key != "" {
		buf = appendVarintField(buf, 1, []byte(req.Key))
	}
	buf = appendInt64Field(buf, 2, req.Tokens)
	buf = appendInt64Field(buf, 3, req.MaxTokens)
	buf = appendInt64Field(buf, 4, req.RefillRate)
	if req.Source != "" {
		buf = appendVarintField(buf, 5, []byte(req.Source))
	}
	return buf
}

func UnmarshalAllowRequest(b []byte, req *AllowRequest) error {
	for len(b) > 0 {
		fieldNum, wireType, rest, err := readKey(b)
		if err != nil {
			return err
		}
		b = rest
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return errors.New("invalid wire type for field 1")
			}
			var data []byte
			data, b, err = readBytes(b)
			if err != nil {
				return err
			}
			req.Key = string(data)
		case 2:
			if wireType != 0 {
				return errors.New("invalid wire type for field 2")
			}
			var v uint64
			v, b, err = readVarint(b)
			if err != nil {
				return err
			}
			req.Tokens = int64(v)
		case 3:
			if wireType != 0 {
				return errors.New("invalid wire type for field 3")
			}
			var v uint64
			v, b, err = readVarint(b)
			if err != nil {
				return err
			}
			req.MaxTokens = int64(v)
		case 4:
			if wireType != 0 {
				return errors.New("invalid wire type for field 4")
			}
			var v uint64
			v, b, err = readVarint(b)
			if err != nil {
				return err
			}
			req.RefillRate = int64(v)
		case 5:
			if wireType != 2 {
				return errors.New("invalid wire type for field 5")
			}
			var data []byte
			data, b, err = readBytes(b)
			if err != nil {
				return err
			}
			req.Source = string(data)
		default:
			var err error
			b, err = skipField(wireType, b)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func MarshalAllowResponse(res *AllowResponse) []byte {
	buf := make([]byte, 0)
	if res.Allowed {
		buf = appendVarint(buf, uint64((1<<3)|0))
		if res.Allowed {
			buf = appendVarint(buf, 1)
		} else {
			buf = appendVarint(buf, 0)
		}
	}
	buf = appendInt64Field(buf, 2, res.RemainingTokens)
	if res.Message != "" {
		buf = appendVarint(buf, uint64((3<<3)|2))
		buf = appendBytes(buf, []byte(res.Message))
	}
	buf = appendInt64Field(buf, 4, res.AllowedHits)
	buf = appendInt64Field(buf, 5, res.DeniedHits)
	if res.LastAllowedAt != "" {
		buf = appendVarint(buf, uint64((6<<3)|2))
		buf = appendBytes(buf, []byte(res.LastAllowedAt))
	}
	if res.LastDeniedAt != "" {
		buf = appendVarint(buf, uint64((7<<3)|2))
		buf = appendBytes(buf, []byte(res.LastDeniedAt))
	}
	if res.Algorithm != "" {
		buf = appendVarint(buf, uint64((8<<3)|2))
		buf = appendBytes(buf, []byte(res.Algorithm))
	}
	if res.StrategyReason != "" {
		buf = appendVarint(buf, uint64((9<<3)|2))
		buf = appendBytes(buf, []byte(res.StrategyReason))
	}
	return buf
}

func UnmarshalAllowResponse(b []byte, res *AllowResponse) error {
	for len(b) > 0 {
		fieldNum, wireType, rest, err := readKey(b)
		if err != nil {
			return err
		}
		b = rest
		switch fieldNum {
		case 1:
			if wireType != 0 {
				return errors.New("invalid wire type for field 1")
			}
			var v uint64
			v, b, err = readVarint(b)
			if err != nil {
				return err
			}
			res.Allowed = v != 0
		case 2:
			if wireType != 0 {
				return errors.New("invalid wire type for field 2")
			}
			var v uint64
			v, b, err = readVarint(b)
			if err != nil {
				return err
			}
			res.RemainingTokens = int64(v)
		case 3:
			if wireType != 2 {
				return errors.New("invalid wire type for field 3")
			}
			var data []byte
			data, b, err = readBytes(b)
			if err != nil {
				return err
			}
			res.Message = string(data)
		case 4:
			if wireType != 0 {
				return errors.New("invalid wire type for field 4")
			}
			var v uint64
			v, b, err = readVarint(b)
			if err != nil {
				return err
			}
			res.AllowedHits = int64(v)
		case 5:
			if wireType != 0 {
				return errors.New("invalid wire type for field 5")
			}
			var v uint64
			v, b, err = readVarint(b)
			if err != nil {
				return err
			}
			res.DeniedHits = int64(v)
		case 6:
			if wireType != 2 {
				return errors.New("invalid wire type for field 6")
			}
			var data []byte
			data, b, err = readBytes(b)
			if err != nil {
				return err
			}
			res.LastAllowedAt = string(data)
		case 7:
			if wireType != 2 {
				return errors.New("invalid wire type for field 7")
			}
			var data []byte
			data, b, err = readBytes(b)
			if err != nil {
				return err
			}
			res.LastDeniedAt = string(data)
		case 8:
			if wireType != 2 {
				return errors.New("invalid wire type for field 8")
			}
			var data []byte
			data, b, err = readBytes(b)
			if err != nil {
				return err
			}
			res.Algorithm = string(data)
		case 9:
			if wireType != 2 {
				return errors.New("invalid wire type for field 9")
			}
			var data []byte
			data, b, err = readBytes(b)
			if err != nil {
				return err
			}
			res.StrategyReason = string(data)
		default:
			var err error
			b, err = skipField(wireType, b)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func appendVarintField(buf []byte, fieldNumber int, data []byte) []byte {
	buf = appendVarint(buf, uint64((fieldNumber<<3)|2))
	buf = appendBytes(buf, data)
	return buf
}

func appendInt64Field(buf []byte, fieldNumber int, value int64) []byte {
	if value < 0 {
		value = 0
	}
	buf = appendVarint(buf, uint64((fieldNumber<<3)|0))
	buf = appendVarint(buf, uint64(value))
	return buf
}

func appendVarint(buf []byte, value uint64) []byte {
	for value >= 0x80 {
		buf = append(buf, byte(value)|0x80)
		value >>= 7
	}
	buf = append(buf, byte(value))
	return buf
}

func appendBytes(buf []byte, data []byte) []byte {
	buf = appendVarint(buf, uint64(len(data)))
	buf = append(buf, data...)
	return buf
}

func readKey(b []byte) (fieldNum int, wireType int, rest []byte, err error) {
	var key uint64
	key, rest, err = readVarint(b)
	if err != nil {
		return
	}
	fieldNum = int(key >> 3)
	wireType = int(key & 0x7)
	return
}

func readVarint(b []byte) (uint64, []byte, error) {
	var value uint64
	var shift uint
	for i, by := range b {
		if shift >= 64 {
			return 0, nil, errors.New("varint overflow")
		}
		value |= uint64(by&0x7F) << shift
		if by < 0x80 {
			return value, b[i+1:], nil
		}
		shift += 7
	}
	return 0, nil, errors.New("unexpected end of buffer")
}

func readBytes(b []byte) ([]byte, []byte, error) {
	length, rest, err := readVarint(b)
	if err != nil {
		return nil, nil, err
	}
	if uint64(len(rest)) < length {
		return nil, nil, errors.New("buffer underflow")
	}
	return rest[:length], rest[length:], nil
}

func skipField(wireType int, b []byte) ([]byte, error) {
	switch wireType {
	case 0:
		_, rest, err := readVarint(b)
		return rest, err
	case 1:
		if len(b) < 8 {
			return nil, errors.New("buffer underflow")
		}
		return b[8:], nil
	case 2:
		length, rest, err := readVarint(b)
		if err != nil {
			return nil, err
		}
		if uint64(len(rest)) < length {
			return nil, errors.New("buffer underflow")
		}
		return rest[length:], nil
	case 5:
		if len(b) < 4 {
			return nil, errors.New("buffer underflow")
		}
		return b[4:], nil
	default:
		return nil, errors.New("unsupported wire type")
	}
}
