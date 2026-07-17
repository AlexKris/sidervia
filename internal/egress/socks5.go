package egress

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/AlexKris/sidervia/internal/routing"
)

func (m *Manager) connectSOCKS5(ctx context.Context, network string, proxy *routing.Proxy, target netip.Addr, targetPort string) (net.Conn, error) {
	proxyAddresses, err := m.resolveConfigured(ctx, proxy.Host)
	if err != nil {
		return nil, err
	}
	connection, err := dialAddresses(ctx, network, strconv.Itoa(proxy.Port), proxyAddresses)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = connection.Close()
		}
	}()
	_ = connection.SetDeadline(time.Now().Add(15 * time.Second))
	methods := []byte{0x00}
	if proxy.Username != "" || proxy.Password != "" {
		methods = append(methods, 0x02)
	}
	if _, err := connection.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return nil, err
	}
	var choice [2]byte
	if _, err := io.ReadFull(connection, choice[:]); err != nil || choice[0] != 0x05 || choice[1] == 0xff {
		return nil, errors.New("SOCKS5 proxy rejected authentication methods")
	}
	if choice[1] == 0x02 {
		if len(proxy.Username) > 255 || len(proxy.Password) > 255 {
			return nil, errors.New("SOCKS5 proxy credentials are too long")
		}
		request := []byte{0x01, byte(len(proxy.Username))}
		request = append(request, proxy.Username...)
		request = append(request, byte(len(proxy.Password)))
		request = append(request, proxy.Password...)
		if _, err := connection.Write(request); err != nil {
			return nil, err
		}
		var response [2]byte
		if _, err := io.ReadFull(connection, response[:]); err != nil || response[1] != 0x00 {
			return nil, errors.New("SOCKS5 proxy authentication failed")
		}
	} else if choice[1] != 0x00 {
		return nil, errors.New("SOCKS5 proxy selected an unsupported authentication method")
	}
	port, err := strconv.ParseUint(targetPort, 10, 16)
	if err != nil || port == 0 {
		return nil, errors.New("SOCKS5 target port is invalid")
	}
	request := []byte{0x05, 0x01, 0x00}
	if target.Is4() {
		request = append(request, 0x01)
		request = append(request, target.AsSlice()...)
	} else {
		request = append(request, 0x04)
		request = append(request, target.AsSlice()...)
	}
	var portBody [2]byte
	binary.BigEndian.PutUint16(portBody[:], uint16(port))
	request = append(request, portBody[:]...)
	if _, err := connection.Write(request); err != nil {
		return nil, err
	}
	var response [4]byte
	if _, err := io.ReadFull(connection, response[:]); err != nil {
		return nil, err
	}
	if response[0] != 0x05 || response[1] != 0x00 {
		return nil, fmt.Errorf("SOCKS5 CONNECT failed with code %d", response[1])
	}
	addressLength := 0
	switch response[3] {
	case 0x01:
		addressLength = 4
	case 0x04:
		addressLength = 16
	case 0x03:
		var length [1]byte
		if _, err := io.ReadFull(connection, length[:]); err != nil {
			return nil, err
		}
		addressLength = int(length[0])
	default:
		return nil, errors.New("SOCKS5 proxy returned an invalid address type")
	}
	if _, err := io.CopyN(io.Discard, connection, int64(addressLength+2)); err != nil {
		return nil, err
	}
	_ = connection.SetDeadline(time.Time{})
	keep = true
	return connection, nil
}
