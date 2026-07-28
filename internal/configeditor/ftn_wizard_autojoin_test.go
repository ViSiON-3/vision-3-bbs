package configeditor

import "testing"

// TestFTNWizardAreaHonorsAutoJoinChoice verifies that message areas created
// by the FTN wizard use the sysop's "Newscan Default" wizard choice for
// AutoJoin, rather than a hardcoded true.
func TestFTNWizardAreaHonorsAutoJoinChoice(t *testing.T) {
	for _, autoJoin := range []bool{true, false} {
		m := Model{configs: &allConfigs{}, ftnWizard: &ftnWizardState{autoJoinAreas: autoJoin}}
		m.createFTNMsgAreaIfNeeded("FSX_GEN", "General", "echomail", "fsxnet", "FSX_GEN", "21:1/100", 0, "msgbases/fsx_gen")
		if len(m.configs.MsgAreas) != 1 {
			t.Fatalf("expected 1 area, got %d", len(m.configs.MsgAreas))
		}
		if m.configs.MsgAreas[0].AutoJoin != autoJoin {
			t.Errorf("AutoJoin = %v, want %v", m.configs.MsgAreas[0].AutoJoin, autoJoin)
		}
	}
}
