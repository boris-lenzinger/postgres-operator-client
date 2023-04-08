package processing

import (
	"encoding/json"
	"fmt"
	"github.com/crunchydata/postgres-operator-client/internal/data"
	"reflect"
	"sort"
	"testing"
	"time"
)

func getJsonStringToBuildBackups() string {
	return `[{"archive":[{"database":{"id":1,"repo-key":2},"id":"13-1","max":"000000240000020B00000045","min":"00000022000001C4000000CA"}],"backup":[{"archive":{"start":"00000022000001C4000000CA","stop":"00000022000001C4000000DB"},"backrest":{"format":5,"version":"2.36"},"database":{"id":1,"repo-key":2},"error":false,"info":{"delta":91597556633,"repository":{"delta":24107897451,"size":24107897451},"size":91597556633},"label":"20230328-000720F","prior":null,"reference":null,"timestamp":{"start":1679962040,"stop":1679967029},"type":"full"},{"archive":{"start":"00000022000001C70000006F","stop":"00000022000001C80000006C"},"backrest":{"format":5,"version":"2.36"},"database":{"id":1,"repo-key":2},"error":false,"info":{"delta":91573777555,"repository":{"delta":24103414055,"size":24103414055},"size":91573777555},"label":"20230329-152616F","prior":null,"reference":null,"timestamp":{"start":1680103576,"stop":1680108699},"type":"full"},{"archive":{"start":"00000023000001D800000059","stop":"00000023000001D800000060"},"backrest":{"format":5,"version":"2.36"},"database":{"id":1,"repo-key":2},"error":false,"info":{"delta":27693209980,"repository":{"delta":7564935040,"size":24101452199},"size":93624902908},"label":"20230329-152616F_20230331-064459D","prior":"20230329-152616F","reference":["20230329-152616F"],"timestamp":{"start":1680245099,"stop":1680247662},"type":"diff"},{"archive":{"start":"00000024000001E6000000C8","stop":"00000024000001E6000000D4"},"backrest":{"format":5,"version":"2.36"},"database":{"id":1,"repo-key":2},"error":false,"info":{"delta":93775567700,"repository":{"delta":23809409688,"size":23809409688},"size":93775567700},"label":"20230331-095540F","prior":null,"reference":null,"timestamp":{"start":1680398831,"stop":1680402603},"type":"full"},{"archive":{"start":"000000240000020900000018","stop":"00000024000002090000001D"},"backrest":{"format":5,"version":"2.36"},"database":{"id":1,"repo-key":2},"error":false,"info":{"delta":34127769497,"repository":{"delta":7688066346,"size":22161385462},"size":87667810495},"label":"20230331-095540F_20230405-052214D","prior":"20230331-095540F","reference":["20230331-095540F"],"timestamp":{"start":1680672134,"stop":1680673825},"type":"diff"}],"cipher":"none","db":[{"id":1,"repo-key":2,"system-id":7115330446670438472,"version":"13"}],"name":"db","repo":[{"cipher":"none","key":2,"status":{"code":0,"message":"ok"}}],"status":{"code":0,"lock":{"backup":{"held":false}},"message":"ok"}}]`
}

func BuildBackups() []data.BackupInfo {
	var backups []data.BackupInfo
	err := json.Unmarshal([]byte(getJsonStringToBuildBackups()), &backups)
	if err != nil {
		panic(fmt.Sprintf("failed to rebuild json from string input: %+v", err))
	}
	return backups
}

func GetOldestStartDateOfBackups(backupInfos data.BackupInfo) time.Time {
	backups := backupInfos.Backups
	sort.Slice(backups, func(i, j int) bool { return backups[i].StopStartTime.Start < backups[j].StopStartTime.Start })
	return time.Unix(0, convertBackupTimestampToNanoSeconds(backups[0].StopStartTime.Start))
}

func Test_timestampIsAfterOneOfThoseBackup(t *testing.T) {
	sampleBackups := BuildBackups()
	type args struct {
		timeRequested time.Time
		backups       data.BackupInfo
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Using a timestamp after the first full backup",
			args: args{
				timeRequested: GetOldestStartDateOfBackups(sampleBackups[0]).Add(10 * time.Hour),
				backups:       sampleBackups[0],
			},
			want: true,
		},
		{
			name: "Using a timestamp before the first full backup",
			args: args{
				timeRequested: GetOldestStartDateOfBackups(sampleBackups[0]).Add(-1 * time.Hour),
				backups:       sampleBackups[0],
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timestampIsAfterOneOfThoseBackup(tt.args.timeRequested, tt.args.backups); got != tt.want {
				t.Errorf("timestampIsAfterOneOfThoseBackup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_computeTimeRequestedInUTC(t *testing.T) {
	type args struct {
		pitr string
	}
	pitrPositiveInUTC, _ := time.Parse("2006-01-02 15:04:05", "2022-12-10 09:01:02")
	pitrNegativeInUTC, _ := time.Parse("2006-01-02 15:04:05", "2022-12-10 11:01:02")
	tests := []struct {
		name    string
		args    args
		want    time.Time
		wantErr bool
	}{
		{
			name: "Compute string with pitr format (including TZ) and get the time in UTC. Case of negative TZ",
			args: args{
				pitr: "2022-12-10 10:01:02-01",
			},
			want:    pitrNegativeInUTC,
			wantErr: false,
		},
		{
			name: "Compute string with pitr format (including TZ) and get the time in UTC. Case of positive TZ",
			args: args{
				pitr: "2022-12-10 10:01:02+01",
			},
			want:    pitrPositiveInUTC,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := computeTimeRequestedInUTCForPitr(tt.args.pitr)
			if (err != nil) != tt.wantErr {
				t.Errorf("computeTimeRequestedInUTCForPitr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("computeTimeRequestedInUTCForPitr() got = %v, want %v", got, tt.want)
			}
		})
	}
}
