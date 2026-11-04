package mailer

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- LogMailer ----------

func TestLogMailer_默认不把正文写进日志(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	m, err := New(Config{Logger: logger})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 没配 SMTP 时 Send 必须失败：返回值是调用方唯一能看到的信号。
	// 以前这里 `return nil`，于是注册响应里的 verificationSent 恒为 true——
	// 用户被告知「验证信已发出」，然后去等一封永远不会来的邮件。
	err = m.Send(context.Background(), "a@example.com", "主题", "验证码 123456")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("未配置 SMTP 时 Send 应返回 ErrNotConfigured，实际: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "未配置 SMTP") {
		t.Fatalf("必须明确指出邮件没有真的发出，实际: %s", out)
	}
	if !strings.Contains(out, "a@example.com") {
		t.Fatalf("应记录收件人，实际: %s", out)
	}
	// 验证码进日志是泄漏，默认必须不出现。
	if strings.Contains(out, "123456") {
		t.Fatalf("默认不应把正文写进日志，实际: %s", out)
	}
}

func TestLogMailer_显式打开才写正文(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	m, err := New(Config{LogCodes: true, Logger: logger})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Send(context.Background(), "a@example.com", "主题", "验证码 123456"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("未配置 SMTP 时仍应返回 ErrNotConfigured，实际: %v", err)
	}
	if !strings.Contains(buf.String(), "123456") {
		t.Fatalf("打开 LogCodes 后应能看到正文，实际: %s", buf.String())
	}
}

// ---------- 配置校验 ----------

func TestNew_配了SMTP但没配发件地址必须失败(t *testing.T) {
	// 从地址是收件方判断「谁发的」的唯一凭据，空着必然被拒收——宁可启动就错。
	if _, err := New(Config{Host: "smtp.example.com", Port: "587"}); err == nil {
		t.Fatal("应当报错")
	}
}

func TestNew_端口不合法必须失败(t *testing.T) {
	for _, p := range []string{"0", "-1", "70000", "abc"} {
		if _, err := New(Config{Host: "smtp.example.com", Port: p, From: "a@b.c"}); err == nil {
			t.Fatalf("端口 %q 应当报错", p)
		}
	}
}

// ---------- 真发一遍：对着进程内的假 SMTP 服务器 ----------

// fakeSMTP 是一个最小的明文 SMTP 服务器，支持 STARTTLS / AUTH PLAIN / DATA。
// 它存在的意义是证明「我们真的会连出去、会先升级 TLS、会认证、会把正文发出去」，
// 而不只是配置能解析。
type fakeSMTP struct {
	ln       net.Listener
	tlsConf  *tls.Config
	mu       sync.Mutex
	gotTo    []string
	gotFrom  string
	authUser string
	gotData  string
	usedTLS  bool
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	cert := selfSigned(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起监听失败: %v", err)
	}
	f := &fakeSMTP{ln: ln, tlsConf: &tls.Config{Certificates: []tls.Certificate{cert}}}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeSMTP) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	var (
		r     = bufio.NewReader(conn)
		tlsOn = false
	)
	write := func(v string) { _, _ = conn.Write([]byte(v + "\r\n")) }

	write("220 fake ESMTP")
	inData := false
	var data strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				inData = false
				f.mu.Lock()
				f.gotData = data.String()
				f.mu.Unlock()
				write("250 OK queued")
				continue
			}
			data.WriteString(line + "\n")
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			if tlsOn {
				_, _ = conn.Write([]byte("250-fake\r\n250-AUTH PLAIN\r\n250 OK\r\n"))
			} else {
				// 明文阶段只广告 STARTTLS：客户端必须先升级，否则拿不到 AUTH。
				_, _ = conn.Write([]byte("250-fake\r\n250-STARTTLS\r\n250 OK\r\n"))
			}
		case upper == "STARTTLS":
			write("220 Ready to start TLS")
			tconn := tls.Server(conn, f.tlsConf)
			if err := tconn.Handshake(); err != nil {
				return
			}
			conn = tconn
			r = bufio.NewReader(conn)
			tlsOn = true
			f.mu.Lock()
			f.usedTLS = true
			f.mu.Unlock()
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			parts := strings.Fields(line)
			if len(parts) == 3 {
				if user, err := decodePlain(parts[2]); err == nil {
					f.mu.Lock()
					f.authUser = user
					f.mu.Unlock()
				}
			}
			write("235 2.7.0 Authentication successful")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			f.mu.Lock()
			f.gotFrom = line
			f.mu.Unlock()
			write("250 OK")
		case strings.HasPrefix(upper, "RCPT TO:"):
			f.mu.Lock()
			f.gotTo = append(f.gotTo, line)
			f.mu.Unlock()
			write("250 OK")
		case upper == "DATA":
			inData = true
			write("354 End data with <CR><LF>.<CR><LF>")
		case upper == "QUIT":
			write("221 Bye")
			return
		default:
			write("250 OK")
		}
	}
}

func TestSMTPMailer_真的把信发出去(t *testing.T) {
	f := startFakeSMTP(t)
	addr := f.ln.Addr().(*net.TCPAddr)

	m, err := New(Config{
		Host:      "127.0.0.1",
		Port:      strconv.Itoa(addr.Port),
		User:      "mailer-user",
		Pass:      "mailer-pass",
		From:      "noreply@deepdemos.top",
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // 自签证书
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Send(ctx, "user@example.com", "登录验证码", "验证码 246810，5 分钟内有效。"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.usedTLS {
		t.Fatal("没有先升级 TLS 就发了信——这正是 TLSMandatory 要挡的事")
	}
	if len(f.gotTo) != 1 || !strings.Contains(f.gotTo[0], "user@example.com") {
		t.Fatalf("收件人不对: %v", f.gotTo)
	}
	if !strings.Contains(f.gotFrom, "noreply@deepdemos.top") {
		t.Fatalf("发件人不对: %s", f.gotFrom)
	}
	if f.authUser != "mailer-user" {
		t.Fatalf("没有用配置的账号认证，实际: %q", f.authUser)
	}
	if !strings.Contains(f.gotData, "246810") {
		t.Fatalf("正文里的验证码没发出去，实际 DATA:\n%s", f.gotData)
	}
}

// ---------- 帮手 ----------

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签证书失败: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func decodePlain(s string) (string, error) {
	raw, err := base64Decode(s)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) < 3 {
		return "", nil
	}
	return parts[1], nil
}

func base64Decode(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
