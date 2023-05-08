package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

type Ovo struct {
	AccountInfo *AccountInfo
	Client      *http.Client
	LoggedIn    bool

	GaugeCache map[string]prometheus.Gauge
}

type SupplyPoint struct {
	Mpxn  string `json:"mpxn"`
	Fuel  string `json:"fuel"`
	Start string `json:"start"`
	Msn   string `json:"msn"`
}

type GasReading struct {
	Volume float64 `json:"gasVolume"`
	Time   string  `json:"readingDateTime"`
}

type ElectricityTierReading struct {
	Reading float64 `json:"meterRegisterReading"`
	Label   string  `json:"timeOfUseLabel"`
}

type ElectricityReading struct {
	Tiers []ElectricityTierReading `json:"tiers"`
	Time  string                   `json:"readingDateTime"`
}

func (o *Ovo) Scan() error {

	// initialize the client if needed
	if o.Client == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return err
		}
		o.Client = &http.Client{
			Jar:     jar,
			Timeout: time.Minute,
		}
	}
	if o.GaugeCache == nil {
		o.GaugeCache = make(map[string]prometheus.Gauge)
	}

	var lastErr error
	for i := 0; i < 3; i++ {
		if lastErr != nil {
			time.Sleep(time.Second * 3)
			zap.S().Warnf("retrying due to error: %v", lastErr)
		}
		if !o.LoggedIn {
			if err := o.Login(); err != nil {
				return fmt.Errorf("failed to login to ovo: %v", err)
			}
		}
		if points, err := o.LoadPoints(); err != nil {
			lastErr = fmt.Errorf("failed to load points: %v", err)
			continue
		} else {
			for _, point := range points {
				if err = o.ScanPoint(&point); err != nil {
					lastErr = fmt.Errorf("failed to scan point: %v", err)
					continue
				}
			}
			lastErr = nil
			break
		}
	}
	return lastErr
}

func (o *Ovo) Login() error {
	zap.S().Info("attempting to log in")
	// attempt to log in
	data, _ := json.Marshal(map[string]interface{}{
		"username":         o.AccountInfo.Username,
		"password":         o.AccountInfo.Password,
		"rememberMe":       false,
		"refreshTokenType": "",
	})
	req, _ := http.NewRequest(http.MethodPost, "https://my.ovoenergy.com/api/v2/auth/login", bytes.NewBuffer(data))
	req.Header.Set("content-type", "application/json")
	resp, err := o.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %v", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		data, _ = io.ReadAll(resp.Body)
		return fmt.Errorf("login request failed: %v %v", resp.StatusCode, string(data))
	}
	zap.S().Info("successfully logged in")
	o.LoggedIn = true
	return nil
}

func (o *Ovo) LoadPoints() ([]SupplyPoint, error) {
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("https://smartpaymapi.ovoenergy.com/orex/api/supply-points/account/%v", o.AccountInfo.AccountNumber), nil)
	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			o.LoggedIn = false
		}
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("supply points query failed: %v %v", resp.StatusCode, string(data))
	}

	points := make([]SupplyPoint, 0)
	if err = json.NewDecoder(resp.Body).Decode(&points); err != nil {
		return nil, fmt.Errorf("failed to decode supply point data: %v", err)
	}
	zap.S().Debugw("scanned points", "points", points)
	return points, nil
}

func (o *Ovo) ScanPoint(point *SupplyPoint) error {
	date := time.Now().Add(-time.Hour * 24 * 64).Format("2006-01-02")
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("https://smartpaymapi.ovoenergy.com/rlc/rac-public-api/api/v5/supplypoints/%s/%s/meters/%s/readings?from=%s", strings.ToLower(point.Fuel), point.Mpxn, point.Msn, date), nil)
	resp, err := o.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to query readings: %v %v", req.URL, err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			o.LoggedIn = false
		}
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to query readings: %v %v %v", req.URL, resp.StatusCode, string(data))
	}

	if strings.ToLower(point.Fuel) == "gas" {
		readings := make([]GasReading, 0)
		if err = json.NewDecoder(resp.Body).Decode(&readings); err != nil {
			return fmt.Errorf("failed to decode gas reading data: %v", err)
		}
		zap.S().Infof("receieved %d recent readings", len(readings))
		zap.S().Debugw("scanned readings", "readings", readings)
		if len(readings) > 0 {
			reading := readings[0]
			zap.S().Infof("last reading is: %v", reading)
			cacheName := point.Mpxn
			gauge, ok := o.GaugeCache[cacheName]
			if !ok {
				gauge = promauto.NewGauge(prometheus.GaugeOpts{
					Name: "ovo_reading_last",
					ConstLabels: map[string]string{
						"fuel": point.Fuel,
						"tier": "default",
						"mpxn": point.Mpxn,
						"msn":  point.Msn,
					},
				})
				o.GaugeCache[cacheName] = gauge
			}
			gauge.Set(reading.Volume)
			if reading.Time != "" {
				if err = o.EmitAge(point, reading.Time); err != nil {
					return fmt.Errorf("failed to emit age of reading: %v", err)
				}
			}
		}
	} else {
		readings := make([]ElectricityReading, 0)
		if err = json.NewDecoder(resp.Body).Decode(&readings); err != nil {
			return fmt.Errorf("failed to decode gas reading data: %v", err)
		}
		zap.S().Infof("receieved %d recent readings", len(readings))
		zap.S().Debugw("scanned readings", "readings", readings)
		if len(readings) > 0 {
			reading := readings[0]
			zap.S().Infof("last reading is: %v", reading)
			for _, tier := range reading.Tiers {
				cacheName := point.Mpxn + "_" + tier.Label
				gauge, ok := o.GaugeCache[cacheName]
				if !ok {
					gauge = promauto.NewGauge(prometheus.GaugeOpts{
						Name: "ovo_reading_last",
						ConstLabels: map[string]string{
							"fuel": point.Fuel,
							"tier": tier.Label,
							"mpxn": point.Mpxn,
							"msn":  point.Msn,
						},
					})
					o.GaugeCache[cacheName] = gauge
				}
				gauge.Set(tier.Reading)
			}
			if reading.Time != "" {
				if err = o.EmitAge(point, reading.Time); err != nil {
					return fmt.Errorf("failed to emit age of reading: %v", err)
				}
			}
		}
	}
	return nil
}

func (o *Ovo) EmitAge(point *SupplyPoint, date string) error {
	readingDate, err := time.Parse("2006-01-02T03:04:05", date)
	if err != nil {
		return fmt.Errorf("failed to parse reading time '%s': %v", date, err)
	}
	cacheName := point.Mpxn + "_age"
	gauge, ok := o.GaugeCache[cacheName]
	if !ok {
		gauge = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "ovo_reading_age_seconds",
			ConstLabels: map[string]string{
				"fuel": point.Fuel,
				"mpxn": point.Mpxn,
				"msn":  point.Msn,
			},
		})
		o.GaugeCache[cacheName] = gauge
	}
	gauge.Set(time.Since(readingDate).Seconds())
	return nil
}
