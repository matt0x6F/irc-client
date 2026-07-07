package storage

import "testing"

func TestActivityIgnoredSenders(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("IgnNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	netID := net.ID

	if err := s.AddIgnoredSender(netID, "ChanServ"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// idempotent re-add
	if err := s.AddIgnoredSender(netID, "ChanServ"); err != nil {
		t.Fatalf("re-add: %v", err)
	}

	// case-insensitive match
	got, err := s.IsSenderIgnored(netID, "chanserv")
	if err != nil {
		t.Fatalf("is-ignored: %v", err)
	}
	if !got {
		t.Fatalf("expected chanserv ignored")
	}

	no, err := s.IsSenderIgnored(netID, "SomeUser")
	if err != nil {
		t.Fatalf("is-ignored2: %v", err)
	}
	if no {
		t.Fatalf("expected SomeUser not ignored")
	}

	byNet, err := s.ListIgnoredSendersByNetwork(netID)
	if err != nil || len(byNet) != 1 || byNet[0] != "ChanServ" {
		t.Fatalf("list-by-net = %v, %v", byNet, err)
	}

	all, err := s.ListAllIgnoredSenders()
	if err != nil || len(all) != 1 || all[0].Nick != "ChanServ" || all[0].NetworkID != netID {
		t.Fatalf("list-all = %+v, %v", all, err)
	}

	if err := s.RemoveIgnoredSender(netID, "CHANSERV"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	after, _ := s.IsSenderIgnored(netID, "ChanServ")
	if after {
		t.Fatalf("expected removed")
	}
}
