package skipservice

/*
The skipservice.go defines what to do for each API-call. This part of the service
runs on the node.
*/

import (
	"errors"
	"sync"
	"time"

	skipchain "github.com/dedis/cothority/skipchain"
	"github.com/nblp/decenarch"

	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
	"gopkg.in/dedis/onet.v1/network"
)

// Used for tests
var templateID onet.ServiceID

func init() {
	var err error
	templateID, err = onet.RegisterNewService(decenarch.SkipServiceName, newService)
	log.ErrFatal(err)
	network.RegisterMessage(&skipstorage{})
}

// Service is our template-service
type SkipService struct {
	// We need to embed the ServiceProcessor, so that incoming messages
	// are correctly handled.
	*onet.ServiceProcessor

	skipstorage *skipstorage
	stopsignal  bool

	data []decenarch.Webstore
}

// storageID reflects the data we're storing - we could store more
// than one structure.
const skipstorageID = "main"

type skipstorage struct {
	sync.Mutex
	LastSkipBlockID skipchain.SkipBlockID
	Skipchain       []*skipchain.SkipBlock
}

// SkipRootStartRequest TODO documentation
func (s *SkipService) SkipRootStartRequest(req *decenarch.SkipRootStartRequest) (*decenarch.SkipRootStartResponse, onet.ClientError) {
	log.Lvl1("SkipRootStartRequest execution")
	skipclient := skipchain.NewClient()
	// here we assume the skipchain will be forwarded to other member of the roster
	skipblock, err := skipclient.CreateGenesis(
		req.Roster,
		2,
		2,
		skipchain.VerificationStandard,
		make([]decenarch.Webstore, 0),
		nil)
	if err != nil {
		return nil, err
	}
	s.skipstorage.Lock()
	s.skipstorage.LastSkipBlockID = skipblock.Hash
	s.skipstorage.Skipchain = append(s.skipstorage.Skipchain, skipblock)
	s.skipstorage.Unlock()
	return &decenarch.SkipRootStartResponse{skipblock}, nil
}

// SkipStartRequest TODO documentation + probably debug/improve things
func (s *SkipService) SkipStartRequest(req *decenarch.SkipStartRequest) (*decenarch.SkipStartResponse, onet.ClientError) {
	skipclient := skipchain.NewClient()
	go func() {
		for !s.stopsignal {
			upresp, uperr := skipclient.GetUpdateChain(
				req.Roster, req.Genesis.Hash)
			if uperr != nil {
				log.Error(uperr)
			} else {
				s.skipstorage.Lock()
				l := len(s.skipstorage.Skipchain)
				s.skipstorage.Skipchain = append(
					s.skipstorage.Skipchain[:l-1],
					upresp.Update...)
				l = len(s.skipstorage.Skipchain)
				s.skipstorage.LastSkipBlockID = s.skipstorage.Skipchain[l-1].Hash
				s.skipstorage.Unlock()
			}
			s.skipstorage.Lock()
			latest := s.skipstorage.Skipchain[len(s.skipstorage.Skipchain)-1]
			s.skipstorage.Unlock()
			resp, err := skipclient.StoreSkipBlock(latest, req.Roster, s.data)
			if err != nil {
				log.Error(err)
			} else {
				s.skipstorage.Lock()
				s.skipstorage.LastSkipBlockID = resp.Latest.Hash
				s.skipstorage.Unlock()
				s.data = make([]decenarch.Webstore, 0)
				s.save()
			}
			time.Sleep(10 * time.Minute)
		}
	}()
	return nil, nil
}

// SkipStopRequest sends a signal to the service to stop the loop of skipblock creation
func (s *SkipService) SkipStopRequest(req *decenarch.SkipStopRequest) (*decenarch.SkipStopResponse, onet.ClientError) {
	s.stopsignal = true
	return &decenarch.SkipStopResponse{}, nil
}

func (s *SkipService) SkipAddDataRequest(req *decenarch.SkipAddDataRequest) (*decenarch.SkipAddDataResponse, onet.ClientError) {
	s.data = append(s.data, req.Data...)
	return &decenarch.SkipAddDataResponse{}, nil
}

func (s *SkipService) SkipGetDataRequest(req *decenarch.SkipGetDataRequest) (*decenarch.SkipGetDataResponse, onet.ClientError) {
	skipclient := skipchain.NewClient()
	resp, err := skipclient.GetAllSkipchains(req.Roster.RandomServerIdentity())
	if err != nil {
		return nil, err
	}
	// TODO extract correct skipchain from resp
	log.Lvl4(resp)
	// TODO extract correct block from url/timestamp
	// TODO extract correct website as well as associated data
	// TODO return the []Webstore retrieved
	return nil, nil
}

// NewProtocol is called on all nodes of a Tree (except the root, since it is
// the one starting the protocol) so it's the Service that will be called to
// generate the PI on all others node.
// If you use CreateProtocolOnet, this will not be called, as the Onet will
// instantiate the protocol on its own. If you need more control at the
// instantiation of the protocol, use CreateProtocolService, and you can
// give some extra-configuration to your protocol in here.
func (s *SkipService) NewProtocol(tn *onet.TreeNodeInstance, conf *onet.GenericConfig) (onet.ProtocolInstance, error) {
	log.Lvl3("Decenarch SkipService new protocol event")
	return nil, nil
}

// saves all skipblocks.
func (s *SkipService) save() {
	s.skipstorage.Lock()
	defer s.skipstorage.Unlock()
	err := s.Save(skipstorageID, s.skipstorage)
	if err != nil {
		log.Error("Couldn't save file:", err)
	}
}

// Tries to load the configuration and updates the data in the service
// if it finds a valid config-file.
func (s *SkipService) tryLoad() error {
	s.skipstorage = &skipstorage{}
	if !s.DataAvailable(skipstorageID) {
		return nil
	}
	msg, err := s.Load(skipstorageID)
	if err != nil {
		return err
	}
	var ok bool
	s.skipstorage, ok = msg.(*skipstorage)
	if !ok {
		return errors.New("Data of wrong type")
	}
	return nil
}

// newService receives the context that holds information about the node it's
// running on. Saving and loading can be done using the context. The data will
// be stored in memory for tests and simulations, and on disk for real deployments.
func newService(c *onet.Context) onet.Service {
	s := &SkipService{
		ServiceProcessor: onet.NewServiceProcessor(c),
		data:             make([]decenarch.Webstore, 0),
	}
	if err := s.RegisterHandlers(
		s.SkipRootStartRequest,
		s.SkipStartRequest,
		s.SkipStopRequest,
		s.SkipAddDataRequest,
		s.SkipGetDataRequest); err != nil {
		log.ErrFatal(err, "Couldn't register messages")
	}
	if err := s.tryLoad(); err != nil {
		log.Error(err)
	}
	return s
}