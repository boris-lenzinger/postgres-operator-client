package processing

import (
	"encoding/json"
	"fmt"
	"github.com/crunchydata/postgres-operator-client/internal/data"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var regexPitr = regexp.MustCompile("^(?P<Year>[0-9]{4})-(?P<Month>[0-9]{2})-(?P<Day>[0-9]{2}) (?P<Hour>[0-9]{2}):(?P<Minute>[0-9]{2}):(?P<Second>[0-9]{2})[+-](?P<TimeZone>[0-9]{2})")

// IsPitrSyntacticallyValid validates that a supplied PITR
// is matching the expected syntax for pgbackrest.
//
// Here are the constraints on the PITR:
// must match : YYYY-MM-DD HH:MM:SS+/-TZ
// TZ must be less or equal than 12
// Month must be less than 12 and larger than 0
// hour must be less than 23 and positive or null
// minute must be less than 60 and positive or null
// seconds must be less than 60 and positive or null
// must not be in the future
// This function does not check if the time is before any backup. This is checked
// in the code after retrieving the cluster and check its backups.
func IsPitrSyntacticallyValid(userPitr string) bool {
	if !regexPitr.MatchString(userPitr) {
		return false
	}
	matches := regexPitr.FindStringSubmatch(userPitr)
	result := make(map[string]string)
	for i, name := range regexPitr.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = matches[i]
		}
	}
	month, _ := strconv.Atoi(result["Month"])
	day, _ := strconv.Atoi(result["Day"])
	hour, _ := strconv.Atoi(result["Hour"])
	minute, _ := strconv.Atoi(result["Minute"])
	second, _ := strconv.Atoi(result["Second"])
	timezone, _ := strconv.Atoi(result["TimeZone"])
	switch {
	case month <= 0 || month > 12:
		return false
	case day <= 0 || day > 31:
		return false
	case hour < 0 || hour > 23:
		return false
	case minute < 0 || minute > 59:
		return false
	case second < 0 || second > 59:
		return false
	case timezone < 0 || timezone > 12:
		return false
	}
	return true
}

func IsValidPitr(restConfig *rest.Config, namespace string, sourceCluster *unstructured.Unstructured, repoName, pitr string) error {
	stdoutAsJson, stderr, err := GetExistingBackups(restConfig, namespace, sourceCluster.GetName(), repoName, "json")
	if err != nil {
		return errors.Wrapf(err, "failed to get backupInfos for cluster %s/%s on repo %q", namespace, sourceCluster.GetName(), repoName)
	}
	if stderr != "" {
		return fmt.Errorf("failed to get backupInfos for cluster %s/%s on repo %q due to %s", namespace, sourceCluster.GetName(), repoName, stderr)
	}

	var backupInfos []data.BackupInfo
	err = json.Unmarshal([]byte(stdoutAsJson), &backupInfos)
	if err != nil {
		return errors.Wrap(err, "failed to unmarshal backup lists")
	}
	// Compute the PITR as a date

	timeRequestedInUTC, err := computeTimeRequestedInUTCForPitr(pitr)

	if timestampIsAfterOneOfThoseBackup(timeRequestedInUTC, backupInfos[0]) {
		return nil
	}
	return fmt.Errorf("the requested PITR is before any full backup for this cluster. Cannot restore before oldest full backup")
}

func timestampIsAfterOneOfThoseBackup(timeRequested time.Time, backupInfos data.BackupInfo) bool {
	for _, backup := range backupInfos.Backups {
		if backup.Type != data.Full {
			continue
		}
		// we are able to restore after a full backup using the full + WALs (or
		// using Full + diff + WALs or Full + incr + WALs)
		startTime := time.Unix(0, convertBackupTimestampToNanoSeconds(backup.StopStartTime.Start))
		if timeRequested.After(startTime) {
			return true
		}
	}
	return false
}

func convertBackupTimestampToNanoSeconds(t int64) int64 {
	return int64(int64(math.Pow(10, 9)) * t)
}

func computeTimeRequestedInUTCForPitr(pitr string) (time.Time, error) {
	submatches := regexPitr.FindStringSubmatch(pitr)
	var year, month, day, hours, minutes, seconds, timezone string
	for i, name := range regexPitr.SubexpNames() {
		switch {
		case i == 0:
			continue
		case name == "Year":
			year = submatches[i]
		case name == "Month":
			month = submatches[i]
		case name == "Day":
			day = submatches[i]
		case name == "Hour":
			hours = submatches[i]
		case name == "Minute":
			minutes = submatches[i]
		case name == "Second":
			seconds = submatches[i]
		case name == "TimeZone":
			timezone = submatches[i]
		}
	}
	timezoneAsInt, err := strconv.Atoi(timezone)
	if err != nil {
		return time.Now(), errors.Wrapf(err, "failed to convert timezone %s as an integer", timezone)
	}

	pitrWithoutTZ := fmt.Sprintf("%s-%s-%s %s:%s:%s", year, month, day, hours, minutes, seconds)
	timeRequested, err := time.Parse("2006-01-02 15:04:05", pitrWithoutTZ)
	multiplier := +1 // if we have a positive timezone, have to shift in negative
	if strings.Index(pitr, "+") != -1 {
		multiplier = -1
	}
	// applying timezone to compute time in UTC. Times are supplied in UTC in
	// timestamps of backups
	timeRequested = timeRequested.Add(time.Duration(multiplier*timezoneAsInt) * time.Hour)
	if err != nil {
		return time.Now(), errors.Wrap(err, "failed to parse pitr date")
	}
	return timeRequested, nil
}
