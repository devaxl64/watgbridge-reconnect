package routeidentity

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func TestEncryptedRouteCanRestoreContactAfterDiskLoss(t *testing.T) {
	k:=make([]byte,32); if _,err:=rand.Read(k);err!=nil { t.Fatal(err) }
	t.Setenv("WATG_BACKUP_KEY",base64.StdEncoding.EncodeToString(k))
	id := "5511999999999@s.whatsapp.net"
	encrypted, err := Encrypt(id)
	if err != nil { t.Fatal(err) }
	if strings.Contains(encrypted,"5511999999999") { t.Fatal("phone number leaked") }
	decoded,found,err := Decrypt(encrypted)
	if err!=nil || !found || decoded!=id { t.Fatalf("round trip: %q %v %v",decoded,found,err) }
	decoded,found,err=Decrypt("Some unrelated group")
	if err!=nil||found||decoded!=""{t.Fatal("unrelated group considered ours")}
}

func TestEncryptedRouteRejectsTamperingAndLostKey(t *testing.T) {
	k:=make([]byte,32); if _,err:=rand.Read(k);err!=nil { t.Fatal(err) }
	t.Setenv("WATG_BACKUP_KEY",base64.StdEncoding.EncodeToString(k))
	encrypted,err:=Encrypt("12345@g.us");if err!=nil{t.Fatal(err)}
	broken:=encrypted[:len(encrypted)-2]+"ab"
	if _,found,err:=Decrypt(broken);!found||err==nil {t.Fatal("tampered identity accepted")}
	t.Setenv("WATG_BACKUP_KEY","")
	if _,found,err:=Decrypt(encrypted);!found||err==nil {t.Fatal("missing secret allowed restoration")}
}
