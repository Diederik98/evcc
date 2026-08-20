package solar

import (
	"math"
	"time"
)

const deg = math.Pi / 180

// solarPosition returns zenith and astronomical azimuth (0 = north, 90 = east)
// for lat/lon in degrees at time t.
func solarPosition(lat, lon float64, t time.Time) (zenith, azimuth float64) {
	t = t.UTC()
	n := julianDate(t) - 2451545.0
	L := math.Mod(280.460+0.9856474*n, 360)
	g := math.Mod(357.528+0.9856003*n, 360) * deg
	lambda := (L + 1.915*math.Sin(g) + 0.020*math.Sin(2*g)) * deg
	epsilon := (23.439 - 0.0000004*n) * deg
	alpha := math.Atan2(math.Cos(epsilon)*math.Sin(lambda), math.Cos(lambda))
	delta := math.Asin(math.Sin(epsilon) * math.Sin(lambda))
	gmst := math.Mod(18.697374558+24.06570982441908*n, 24)
	if gmst < 0 {
		gmst += 24
	}
	lst := math.Mod(gmst+lon/15, 24)
	if lst < 0 {
		lst += 24
	}
	h := lst*15*deg - alpha
	latr := lat * deg
	sinAlt := math.Sin(latr)*math.Sin(delta) + math.Cos(latr)*math.Cos(delta)*math.Cos(h)
	alt := math.Asin(clamp(sinAlt, -1, 1))
	zenith = 90 - alt/deg
	az := math.Atan2(-math.Sin(h), math.Cos(latr)*math.Tan(delta)-math.Sin(latr)*math.Cos(h))
	azimuth = math.Mod(az/deg+360, 360)
	return zenith, azimuth
}

func julianDate(t time.Time) float64 {
	y, m, d := t.Date()
	hour := float64(t.Hour()) + float64(t.Minute())/60 + float64(t.Second())/3600
	if m <= 2 {
		y--
		m += 12
	}
	A := math.Floor(float64(y) / 100)
	B := 2 - A + math.Floor(A/4)
	return math.Floor(365.25*float64(y+4716)) + math.Floor(30.6001*float64(m+1)) + float64(d) + hour/24 + B - 1524.5
}

// clearSkyPOA is a clear-sky plane-of-array irradiance in W/m².
// azFS uses forecast.solar convention: 0 = south, -90 = east, 90 = west.
func clearSkyPOA(lat, lon, tilt, azFS float64, t time.Time) float64 {
	zenith, azAstro := solarPosition(lat, lon, t)
	if zenith >= 90 {
		return 0
	}
	z := zenith * deg
	cosZ := math.Cos(z)
	if cosZ <= 0 {
		return 0
	}

	i0 := 1367 * (1 + 0.033*math.Cos(2*math.Pi*float64(t.YearDay())/365))
	dni := i0 * math.Exp(-0.14/max(cosZ, 0.04))
	dhi := 0.10 * i0 * math.Sqrt(cosZ)

	tiltR := tilt * deg
	surfAz := (azFS + 180) * deg
	sunAz := azAstro * deg
	cosInc := cosZ*math.Cos(tiltR) + math.Sin(z)*math.Sin(tiltR)*math.Cos(sunAz-surfAz)
	poa := dni*max(0, cosInc) + dhi*(1+math.Cos(tiltR))/2
	return max(0, poa)
}

func clamp(v, lo, hi float64) float64 {
	return min(hi, max(lo, v))
}
