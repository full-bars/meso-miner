package connect

// The fork has no application-delivery observer on dialed connections (upstream
// wraps H1 message connections to score dial strategies). unobservedH1MessageConn
// is the seam the H1+ framing code calls to reach the concrete framing type; with
// no wrapper in this tree it returns the connection unchanged.
func unobservedH1MessageConn(conn H1MessageConn) H1MessageConn {
	return conn
}
