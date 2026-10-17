package connect

// Leaf additions extracted from upstream net_http.go at 20b29f79 for the
// udp-candidates port, in fork-adapted (inert) form: the fork's dialer set is
// walked via the exposed HttpClient instead of upstream's dialer.Close(). No
// production caller changes; merge when the L3 transport core lands.

// CloseIdleConnections closes every dialer's pooled HTTP connections and the
// internal DoH resolver's, keeping the strategy usable.
func (self *ClientStrategy) CloseIdleConnections() {
	self.mutex.Lock()
	for dialer := range self.dialers {
		if client := dialer.HttpClient(); client != nil {
			client.CloseIdleConnections()
		}
	}
	self.mutex.Unlock()
	if self.internalDohResolver != nil {
		self.internalDohResolver.CloseIdleConnections()
	}
}

// Close releases the strategy's pooled connections and its internal DoH
// resolver. The fork's strategy carries no cancel or listener-unsubscribe
// fields yet, so this closes what exists.
func (self *ClientStrategy) Close() {
	self.CloseIdleConnections()
	if self.internalDohResolver != nil {
		self.internalDohResolver.Close()
	}
}

// DohSettings returns the strategy's current DoH settings (ported from
// upstream net_http.go at 20b29f79).
func (self *ClientStrategy) DohSettings() *DohSettings {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.dohSettings
}

// SetInternalDohSettings replaces the DoH settings of a running strategy: the
// internal DoH cache its protected control names resolve through is rebuilt
// from them (ported from upstream net_http.go at 20b29f79).
func (self *ClientStrategy) SetInternalDohSettings(dohSettings *DohSettings) {
	if dohSettings == nil {
		dohSettings = DefaultDohSettings()
	}
	replacedCache := func() *DohCache {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		self.dohSettings = dohSettings
		if self.internalDohResolver == nil {
			return nil
		}
		return self.internalDohResolver.replaceCache(NewDohCache(internalDohSettings(dohSettings)))
	}()
	// closed outside the lock: closing joins the requests in flight on it
	if replacedCache != nil {
		replacedCache.Close()
	}
}
