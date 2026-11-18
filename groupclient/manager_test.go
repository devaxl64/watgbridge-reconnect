package groupclient

import (
	"errors"
	"testing"
)

func TestConfigurationAndLoginValidation(t *testing.T) {
	t.Setenv("WATG_TG_APP_ID","")
	t.Setenv("WATG_TG_APP_HASH","")
	t.Setenv("WATG_TG_PHONE","")
	manager,err:=NewFromEnvironment(nil)
	if err!=nil || manager!=nil {t.Fatalf("optional session should remain disabled: %v",err)}
	t.Setenv("WATG_TG_APP_ID","123")
	t.Setenv("WATG_TG_APP_HASH","not-a-real-hash")
	t.Setenv("WATG_TG_PHONE","+5511999999999")
	manager,err=NewFromEnvironment(nil)
	if err!=nil || manager==nil {t.Fatalf("failed to configure: %v",err)}
	if err:=manager.SubmitCode("12345");!errors.Is(err,ErrCodeNotRequested) {t.Fatalf("unexpected code accepted: %v",err)}
	if err:=manager.BeginLogin();err!=nil {t.Fatal(err)}
	if err:=manager.BeginLogin();err==nil {t.Fatal("duplicate login allowed")}
}
