package kit

const (
	FakeMembership   = 0
	StringMembership = 1
	FileMembership   = 2
)

type MirConfig struct {
	Delay              int
	MembershipFileName string
	MembershipType     int
	MembershipFilename string
	Databases          []*TestDB
	MockedTransport    bool
}
