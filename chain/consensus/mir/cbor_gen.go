// Code generated by github.com/whyrusleeping/cbor-gen. DO NOT EDIT.

package mir

import (
	"fmt"
	"io"
	"math"
	"sort"

	abi "github.com/filecoin-project/go-state-types/abi"
	cid "github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

var _ = xerrors.Errorf
var _ = cid.Undef
var _ = math.E
var _ = sort.Sort

var lengthBufCheckpoint = []byte{132}

func (t *Checkpoint) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write(lengthBufCheckpoint); err != nil {
		return err
	}

	// t.Height (abi.ChainEpoch) (int64)
	if t.Height >= 0 {
		if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Height)); err != nil {
			return err
		}
	} else {
		if err := cw.WriteMajorTypeHeader(cbg.MajNegativeInt, uint64(-t.Height-1)); err != nil {
			return err
		}
	}

	// t.BlockCids ([]cid.Cid) (slice)
	if len(t.BlockCids) > cbg.MaxLength {
		return xerrors.Errorf("Slice value in field t.BlockCids was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.BlockCids))); err != nil {
		return err
	}
	for _, v := range t.BlockCids {
		if err := cbg.WriteCid(w, v); err != nil {
			return xerrors.Errorf("failed writing cid field t.BlockCids: %w", err)
		}
	}

	// t.Parent (mir.ParentMeta) (struct)
	if err := t.Parent.MarshalCBOR(cw); err != nil {
		return err
	}

	// t.NextConfigNumber (uint64) (uint64)

	if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.NextConfigNumber)); err != nil {
		return err
	}

	return nil
}

func (t *Checkpoint) UnmarshalCBOR(r io.Reader) (err error) {
	*t = Checkpoint{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 4 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Height (abi.ChainEpoch) (int64)
	{
		maj, extra, err := cr.ReadHeader()
		var extraI int64
		if err != nil {
			return err
		}
		switch maj {
		case cbg.MajUnsignedInt:
			extraI = int64(extra)
			if extraI < 0 {
				return fmt.Errorf("int64 positive overflow")
			}
		case cbg.MajNegativeInt:
			extraI = int64(extra)
			if extraI < 0 {
				return fmt.Errorf("int64 negative oveflow")
			}
			extraI = -1 - extraI
		default:
			return fmt.Errorf("wrong type for int64 field: %d", maj)
		}

		t.Height = abi.ChainEpoch(extraI)
	}
	// t.BlockCids ([]cid.Cid) (slice)

	maj, extra, err = cr.ReadHeader()
	if err != nil {
		return err
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("t.BlockCids: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}

	if extra > 0 {
		t.BlockCids = make([]cid.Cid, extra)
	}

	for i := 0; i < int(extra); i++ {

		c, err := cbg.ReadCid(cr)
		if err != nil {
			return xerrors.Errorf("reading cid field t.BlockCids failed: %w", err)
		}
		t.BlockCids[i] = c
	}

	// t.Parent (mir.ParentMeta) (struct)

	{

		if err := t.Parent.UnmarshalCBOR(cr); err != nil {
			return xerrors.Errorf("unmarshaling t.Parent: %w", err)
		}

	}
	// t.NextConfigNumber (uint64) (uint64)

	{

		maj, extra, err = cr.ReadHeader()
		if err != nil {
			return err
		}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for uint64 field")
		}
		t.NextConfigNumber = uint64(extra)

	}
	return nil
}

var lengthBufParentMeta = []byte{130}

func (t *ParentMeta) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write(lengthBufParentMeta); err != nil {
		return err
	}

	// t.Height (abi.ChainEpoch) (int64)
	if t.Height >= 0 {
		if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Height)); err != nil {
			return err
		}
	} else {
		if err := cw.WriteMajorTypeHeader(cbg.MajNegativeInt, uint64(-t.Height-1)); err != nil {
			return err
		}
	}

	// t.Cid (cid.Cid) (struct)

	if err := cbg.WriteCid(cw, t.Cid); err != nil {
		return xerrors.Errorf("failed to write cid field t.Cid: %w", err)
	}

	return nil
}

func (t *ParentMeta) UnmarshalCBOR(r io.Reader) (err error) {
	*t = ParentMeta{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 2 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Height (abi.ChainEpoch) (int64)
	{
		maj, extra, err := cr.ReadHeader()
		var extraI int64
		if err != nil {
			return err
		}
		switch maj {
		case cbg.MajUnsignedInt:
			extraI = int64(extra)
			if extraI < 0 {
				return fmt.Errorf("int64 positive overflow")
			}
		case cbg.MajNegativeInt:
			extraI = int64(extra)
			if extraI < 0 {
				return fmt.Errorf("int64 negative oveflow")
			}
			extraI = -1 - extraI
		default:
			return fmt.Errorf("wrong type for int64 field: %d", maj)
		}

		t.Height = abi.ChainEpoch(extraI)
	}
	// t.Cid (cid.Cid) (struct)

	{

		c, err := cbg.ReadCid(cr)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Cid: %w", err)
		}

		t.Cid = c

	}
	return nil
}

var lengthBufVoteRecord = []byte{131}

func (t *VoteRecord) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write(lengthBufVoteRecord); err != nil {
		return err
	}

	// t.ConfigurationNumber (uint64) (uint64)

	if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.ConfigurationNumber)); err != nil {
		return err
	}

	// t.ValSetHash (string) (string)
	if len(t.ValSetHash) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.ValSetHash was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.ValSetHash))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.ValSetHash)); err != nil {
		return err
	}

	// t.VotedValidators ([]mir.VotedValidator) (slice)
	if len(t.VotedValidators) > cbg.MaxLength {
		return xerrors.Errorf("Slice value in field t.VotedValidators was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.VotedValidators))); err != nil {
		return err
	}
	for _, v := range t.VotedValidators {
		if err := v.MarshalCBOR(cw); err != nil {
			return err
		}
	}
	return nil
}

func (t *VoteRecord) UnmarshalCBOR(r io.Reader) (err error) {
	*t = VoteRecord{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 3 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.ConfigurationNumber (uint64) (uint64)

	{

		maj, extra, err = cr.ReadHeader()
		if err != nil {
			return err
		}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for uint64 field")
		}
		t.ConfigurationNumber = uint64(extra)

	}
	// t.ValSetHash (string) (string)

	{
		sval, err := cbg.ReadString(cr)
		if err != nil {
			return err
		}

		t.ValSetHash = string(sval)
	}
	// t.VotedValidators ([]mir.VotedValidator) (slice)

	maj, extra, err = cr.ReadHeader()
	if err != nil {
		return err
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("t.VotedValidators: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}

	if extra > 0 {
		t.VotedValidators = make([]VotedValidator, extra)
	}

	for i := 0; i < int(extra); i++ {

		var v VotedValidator
		if err := v.UnmarshalCBOR(cr); err != nil {
			return err
		}

		t.VotedValidators[i] = v
	}

	return nil
}

var lengthBufVotedValidator = []byte{129}

func (t *VotedValidator) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write(lengthBufVotedValidator); err != nil {
		return err
	}

	// t.ID (string) (string)
	if len(t.ID) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.ID was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.ID))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.ID)); err != nil {
		return err
	}
	return nil
}

func (t *VotedValidator) UnmarshalCBOR(r io.Reader) (err error) {
	*t = VotedValidator{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 1 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.ID (string) (string)

	{
		sval, err := cbg.ReadString(cr)
		if err != nil {
			return err
		}

		t.ID = string(sval)
	}
	return nil
}

var lengthBufVoteRecords = []byte{129}

func (t *VoteRecords) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write(lengthBufVoteRecords); err != nil {
		return err
	}

	// t.Records ([]mir.VoteRecord) (slice)
	if len(t.Records) > cbg.MaxLength {
		return xerrors.Errorf("Slice value in field t.Records was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.Records))); err != nil {
		return err
	}
	for _, v := range t.Records {
		if err := v.MarshalCBOR(cw); err != nil {
			return err
		}
	}
	return nil
}

func (t *VoteRecords) UnmarshalCBOR(r io.Reader) (err error) {
	*t = VoteRecords{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 1 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Records ([]mir.VoteRecord) (slice)

	maj, extra, err = cr.ReadHeader()
	if err != nil {
		return err
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("t.Records: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}

	if extra > 0 {
		t.Records = make([]VoteRecord, extra)
	}

	for i := 0; i < int(extra); i++ {

		var v VoteRecord
		if err := v.UnmarshalCBOR(cr); err != nil {
			return err
		}

		t.Records[i] = v
	}

	return nil
}
