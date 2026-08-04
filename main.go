package main

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	HOST          = "api19.tiktokv.com"
	DEADLINE      = 5 * int64(time.Second)
	MAX_THREADS   = 5000
	UPDATE_INTERVAL = 5 * time.Second // Update stats every 5 seconds while sending
)

var (
	BUILDS = []int{247, 312, 322, 357, 358, 415, 422, 444, 466}
	vrgx   = regexp.MustCompile(`(?:/video/|v=)(\d{10,30})`)
)

// Stats tracking
type GlobalStats struct {
	totalViews   atomic.Int64
	totalFailed  atomic.Int64
	totalErrors  atomic.Int64
	startTime    time.Time
	lastReset    time.Time
	mu           sync.RWMutex
	dailyViews   map[string]int64
}

// Store active stat messages for auto-refresh
type ActiveStatMessage struct {
	ChannelID string
	MessageID string
	Session   *discordgo.Session
}

var (
	stats        = &GlobalStats{
		startTime:  time.Now(),
		lastReset:  time.Now(),
		dailyViews: make(map[string]int64),
	}
	activeStats   = make(map[string]*ActiveStatMessage)
	statsMutex    sync.Mutex
)

func (s *GlobalStats) AddViews(count int64) {
	s.totalViews.Add(count)
	s.mu.Lock()
	defer s.mu.Unlock()
	today := time.Now().Format("2006-01-02")
	s.dailyViews[today] += count
}

func (s *GlobalStats) AddFailed(count int64) {
	s.totalFailed.Add(count)
}

func (s *GlobalStats) AddErrors(count int64) {
	s.totalErrors.Add(count)
}

func (s *GlobalStats) GetStats() (total, failed, errors int64, uptime time.Duration, daily string) {
	total = s.totalViews.Load()
	failed = s.totalFailed.Load()
	errors = s.totalErrors.Load()
	uptime = time.Since(s.startTime)
	
	s.mu.RLock()
	defer s.mu.RUnlock()
	today := time.Now().Format("2006-01-02")
	dailyViews := s.dailyViews[today]
	daily = fmt.Sprintf("%d", dailyViews)
	return
}

func (s *GlobalStats) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalViews.Store(0)
	s.totalFailed.Store(0)
	s.totalErrors.Store(0)
	s.startTime = time.Now()
	s.dailyViews = make(map[string]int64)
}

type TTDEV struct {
	Did  string `json:"device_id"`
	Iid  string `json:"iid"`
	Dev  string `json:"device_type"`
	Brnd string `json:"device_brand"`
	Osv  string `json:"os_version"`
	Vc   string `json:"version_code"`
	Reg  string `json:"region"`
	Ch   string `json:"channel"`
	App  string `json:"app_name"`
	Ua   string `json:"user_agent"`
}

type DeviceModel struct {
	Model string
	Brand string
	Res   string
	DPI   string
}

var androidModels = []DeviceModel{
	{"SM-F926B", "samsung", "1080x2400", "480"},
	{"SM-G998B", "samsung", "1440x3200", "560"},
	{"SM-G991B", "samsung", "1080x2400", "420"},
	{"SM-A536B", "samsung", "1080x2400", "400"},
	{"SM-A546B", "samsung", "1080x2400", "400"},
	{"SM-S918B", "samsung", "1440x3120", "550"},
	{"SM-G781B", "samsung", "1080x2400", "420"},
	{"Pixel 7", "google", "1080x2400", "440"},
	{"Pixel 6", "google", "1080x2400", "440"},
	{"Pixel 8", "google", "1080x2400", "480"},
	{"Pixel 8 Pro", "google", "1440x3120", "550"},
	{"Pixel 7a", "google", "1080x2400", "440"},
	{"Redmi Note 12", "xiaomi", "1080x2400", "440"},
	{"Redmi Note 11", "xiaomi", "1080x2400", "400"},
	{"Redmi Note 13", "xiaomi", "1080x2400", "440"},
	{"POCO F5", "xiaomi", "1080x2400", "440"},
	{"POCO F6", "xiaomi", "1080x2400", "440"},
	{"POCO X5", "xiaomi", "1080x2400", "440"},
	{"OnePlus 11", "oneplus", "1440x3216", "525"},
	{"OnePlus 10", "oneplus", "1440x3216", "525"},
	{"OnePlus 12", "oneplus", "1440x3216", "525"},
	{"OnePlus 9", "oneplus", "1440x3216", "525"},
	{"OPPO Find X5", "oppo", "1080x2400", "450"},
	{"OPPO Find X6", "oppo", "1080x2400", "450"},
	{"OPPO Reno 8", "oppo", "1080x2400", "450"},
	{"vivo X80", "vivo", "1080x2400", "440"},
	{"vivo X90", "vivo", "1080x2400", "440"},
	{"vivo Y78", "vivo", "1080x2400", "400"},
	{"Honor 70", "honor", "1080x2400", "430"},
	{"Honor 90", "honor", "1080x2400", "430"},
	{"Honor Magic5", "honor", "1080x2400", "430"},
	{"Nothing Phone (2)", "nothing", "1080x2400", "440"},
	{"Nothing Phone (1)", "nothing", "1080x2400", "440"},
	{"Realme GT 2", "realme", "1080x2400", "440"},
	{"Realme 11", "realme", "1080x2400", "400"},
	{"Motorola Edge 40", "motorola", "1080x2400", "440"},
	{"Motorola Razr 40", "motorola", "1080x2400", "440"},
	{"Sony Xperia 1 V", "sony", "1080x2400", "440"},
	{"Sony Xperia 5 V", "sony", "1080x2400", "440"},
	{"Nokia X30", "nokia", "1080x2400", "400"},
	{"Nokia G60", "nokia", "1080x2400", "400"},
}

var iosModels = []DeviceModel{
	{"iPhone14,5", "apple", "1170x2532", "460"},
	{"iPhone14,2", "apple", "1170x2532", "460"},
	{"iPhone14,3", "apple", "1170x2532", "460"},
	{"iPhone14,4", "apple", "1170x2532", "460"},
	{"iPhone14,6", "apple", "1170x2532", "460"},
	{"iPhone14,7", "apple", "1170x2532", "460"},
	{"iPhone14,8", "apple", "1284x2778", "458"},
	{"iPad13,4", "apple", "2048x2732", "320"},
	{"iPad13,1", "apple", "1640x2360", "264"},
	{"iPad13,2", "apple", "1640x2360", "264"},
	{"iPad13,10", "apple", "2388x1668", "264"},
	{"iPad13,11", "apple", "2388x1668", "264"},
}

var laptopModels = []DeviceModel{
	{"MacBookPro18,1", "apple", "2560x1600", "220"},
	{"MacBookPro18,2", "apple", "3024x1964", "254"},
	{"MacBookPro18,3", "apple", "3456x2234", "254"},
	{"MacBookPro18,4", "apple", "3024x1964", "254"},
	{"MacBookAir10,1", "apple", "2560x1600", "227"},
	{"XPS-15-9520", "dell", "1920x1200", "180"},
	{"XPS-13-9310", "dell", "1920x1200", "166"},
	{"ThinkPad-X1-Carbon-Gen10", "lenovo", "1920x1200", "170"},
	{"ThinkPad-X1-Carbon-Gen9", "lenovo", "1920x1200", "170"},
	{"Surface-Laptop-5", "microsoft", "2256x1504", "201"},
	{"Surface-Pro-9", "microsoft", "2688x1920", "267"},
	{"HP-Spectre-x360", "hp", "1920x1280", "166"},
	{"ASUS-ZenBook", "asus", "1920x1200", "166"},
}

var regions = []string{
	"US", "CA", "GB", "DE", "FR", "IT", "ES", "NL", "SE", "NO", "DK", "FI", "PL", "CZ", "AT", "CH", "BE",
	"IN", "CN", "JP", "KR", "SG", "TH", "ID", "MY", "PH", "VN", "TW", "HK", "AE", "SA", "IL",
	"BR", "AR", "MX", "CO", "PE", "CL", "VE", "EC", "BO", "UY", "PY",
	"ZA", "EG", "NG", "KE", "MA", "GH", "TN", "DZ", "AO", "ET", "TZ", "UG",
	"AU", "NZ", "FJ", "PG", "GU", "HI", "FJ", "SB", "VU", "NC", "PF",
	"RU", "TR", "UA", "KZ", "UZ", "KG", "TJ", "TM", "GE", "AM", "AZ",
}

var (
	devPool  []TTDEV
	devIndex atomic.Int64
)

func init() {
	devPool = make([]TTDEV, 50000)
	for i := range devPool {
		devPool[i] = generateTTDEV()
	}
}

func getDevice() TTDEV {
	return devPool[devIndex.Add(1)%int64(len(devPool))]
}

func randDigits(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('0' + rand.Intn(10))
	}
	return string(b)
}

func generateTTDEV() TTDEV {
	dt := rand.Intn(3)
	var model, brand, osv, ua, ch string
	switch dt {
	case 0:
		s := androidModels[rand.Intn(len(androidModels))]
		model, brand = s.Model, s.Brand
		osv = []string{"10", "11", "12", "13", "14"}[rand.Intn(5)]
		ua = fmt.Sprintf("com.ss.android.ugc.trill/300804 (Linux; U; Android %s; en_US; %s; Build/RP1A.200720.012; Cronet/58.0.2991.0)", osv, model)
		ch = "googleplay"
	case 1:
		s := iosModels[rand.Intn(len(iosModels))]
		model, brand = s.Model, s.Brand
		osv = []string{"14.8", "15.6", "16.2", "17.0"}[rand.Intn(4)]
		ua = fmt.Sprintf("Trill/300804 (iOS %s)", osv)
		ch = "appstore"
	default:
		s := laptopModels[rand.Intn(len(laptopModels))]
		model, brand = s.Model, s.Brand
		osv = []string{"10", "11", "12"}[rand.Intn(3)]
		ua = fmt.Sprintf("Mozilla/5.0 (%s; OS %s)", brand, osv)
		ch = "desktop"
	}
	return TTDEV{
		Did:  randDigits(19),
		Iid:  randDigits(19),
		Dev:  model,
		Brnd: brand,
		Osv:  osv,
		Vc:   "300804",
		Reg:  regions[rand.Intn(len(regions))],
		Ch:   ch,
		App:  "trill",
		Ua:   ua,
	}
}

type tiktokStats struct {
	s, f, t, e atomic.Int64
}

type clientPool struct {
	clients []*http.Client
	next    int32
}

func newClientPool(n, conns int) *clientPool {
	cp := &clientPool{clients: make([]*http.Client, n)}
	per := (conns + n - 1) / n
	for i := 0; i < n; i++ {
		tp := &http.Transport{
			MaxIdleConns:        per,
			MaxIdleConnsPerHost: per,
			MaxConnsPerHost:     per + 5,
		}
		cp.clients[i] = &http.Client{Timeout: time.Duration(DEADLINE), Transport: tp}
	}
	return cp
}

func (cp *clientPool) Get() *http.Client {
	return cp.clients[atomic.AddInt32(&cp.next, 1)%int32(len(cp.clients))]
}

func getvid(l string) (string, error) {
	l = strings.TrimSpace(l)
	if matched, _ := regexp.MatchString(`^\d{10,30}$`, l); matched {
		return l, nil
	}
	if !strings.Contains(l, "tiktok.com/") {
		return "", fmt.Errorf("not a tiktok link")
	}
	c := &http.Client{Timeout: time.Second * 10, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}}
	resp, err := c.Get(l)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	m := vrgx.FindStringSubmatch(resp.Request.URL.String())
	if len(m) < 2 {
		return "", fmt.Errorf("couldnt find id")
	}
	return m[1], nil
}

func sendview(c *http.Client, vid string, st *tiktokStats) {
	build := BUILDS[rand.Intn(len(BUILDS))]
	d := getDevice()
	osv := rand.Intn(7) + 5
	p := url.Values{
		"app_language":    {"fr"},
		"iid":             {d.Iid},
		"device_id":       {d.Did},
		"channel":         {d.Ch},
		"device_type":     {d.Dev},
		"ac":              {"wifi"},
		"os_version":      {strconv.Itoa(osv)},
		"version_code":    {strconv.Itoa(build)},
		"app_name":        {d.App},
		"device_brand":    {d.Brnd},
		"ssmix":           {"a"},
		"device_platform": {"android"},
		"aid":             {"1180"},
		"as":              {"a1iosdfgh"},
		"cp":              {"androide1"},
	}
	h := map[string]string{
		"Host":            HOST,
		"Connection":      "keep-alive",
		"Accept-Encoding": "gzip",
		"X-SS-REQ-TICKET": strconv.FormatInt(time.Now().UnixMilli(), 10),
		"Content-Type":    "application/x-www-form-urlencoded; charset=UTF-8",
		"User-Agent":      d.Ua,
	}
	b := url.Values{
		"manifest_version_code": {strconv.Itoa(build)},
		"update_version_code":   {strconv.Itoa(build) + "0"},
		"play_delta":            {"1"},
		"item_id":               {vid},
		"version_code":          {strconv.Itoa(build)},
		"aweme_type":            {"0"},
	}
	u := fmt.Sprintf("https://%s/aweme/v1/aweme/stats?%s", HOST, p.Encode())
	req, _ := http.NewRequest("POST", u, bytes.NewBufferString(b.Encode()))
	for k, v := range h {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "timeout") {
			st.t.Add(1)
		} else {
			st.e.Add(1)
			stats.AddErrors(1)
		}
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 200 && strings.Contains(resp.Header.Get("Content-Type"), "charset=utf-8") {
		st.s.Add(1)
	} else {
		st.f.Add(1)
		stats.AddFailed(1)
	}
}

func runTikTok(vid string, tgt int, updateChan chan<- int64) int64 {
	start := time.Now()
	st := &tiktokStats{}
	pool := newClientPool(10, 3000)
	done := make(chan struct{})
	stopChan := make(chan struct{})

	// Start a goroutine to update stats every 5 seconds
	go func() {
		ticker := time.NewTicker(UPDATE_INTERVAL)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Send current count to update channel
				sent := st.s.Load()
				updateChan <- sent
			case <-stopChan:
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 3000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if st.s.Load() >= int64(tgt) {
					return
				}
				sendview(pool.Get(), vid, st)
			}
		}()
	}
	wg.Wait()
	close(done)
	close(stopChan)

	sent := st.s.Load()
	stats.AddViews(sent)
	
	elapsed := time.Since(start).Seconds()
	fmt.Printf("Views sent: %d in %.2f seconds (%.0f views/sec)\n", sent, elapsed, float64(sent)/elapsed)
	
	// Send final update
	updateChan <- sent
	
	// Update all active stat messages
	updateAllStats()
	
	return sent
}

// Update all active stat messages
func updateAllStats() {
	statsMutex.Lock()
	defer statsMutex.Unlock()
	
	for key, active := range activeStats {
		if active.Session == nil {
			delete(activeStats, key)
			continue
		}
		
		total, _, _, uptime, daily := stats.GetStats()
		
		message := fmt.Sprintf(
			"📊 **TikTok View Bot Statistics**\n"+
			"═══════════════════════════\n"+
			"📈 Views today: **%s**\n"+
			"📊 Total views: **%d**\n"+
			"⏱️ Uptime: **%s**\n"+
			"🔄 Last reset: **%s**\n"+
			"⏰ **Auto-refreshes every minute**\n"+
			"🕐 Last updated: **%s**",
			daily, total, formatDuration(uptime),
			stats.lastReset.Format("2006-01-02 15:04:05"),
			time.Now().Format("15:04:05"),
		)
		
		_, err := active.Session.ChannelMessageEdit(active.ChannelID, active.MessageID, message)
		if err != nil {
			// If error, remove this stat message from tracking
			delete(activeStats, key)
		}
	}
}

// Start auto-refresh goroutine
func startAutoRefresh() {
	ticker := time.NewTicker(60 * time.Second)
	go func() {
		for range ticker.C {
			updateAllStats()
		}
	}()
}

// ============================================
// DISCORD BOT - PUT YOUR TOKEN HERE
// ============================================

func main() {
	// ⬇️⬇️⬇️ PUT YOUR TOKEN IN THE NEXT LINE (between the quotes) ⬇️⬇️⬇️
	botToken := "MTUzNDIwNjQ1ODYzMDExMTQxMw.G50HBl.88CUZlb143Uy_xyugqzjghL7RHw5CoPGn_SByM"
	// ⬆️⬆️⬆️ REPLACE "YOUR_BOT_TOKEN_HERE" WITH YOUR ACTUAL TOKEN ⬆️⬆️⬆️

	if botToken == "" || botToken == "YOUR_BOT_TOKEN_HERE" {
		fmt.Println("❌ Error: Please put your Discord bot token in the code!")
		fmt.Println("📍 Open main.go and replace 'YOUR_BOT_TOKEN_HERE' with your token")
		return
	}

	dg, err := discordgo.New("Bot " + botToken)
	if err != nil {
		fmt.Println("Error creating Discord session:", err)
		return
	}

	dg.AddHandler(readyHandler)
	dg.AddHandler(interactionCreateHandler)

	err = dg.Open()
	if err != nil {
		fmt.Println("Error opening connection:", err)
		return
	}

	fmt.Println("✅ Bot is now running. Press CTRL+C to exit.")

	// Start auto-refresh for all stats messages
	startAutoRefresh()

	// Register slash commands
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "views",
			Description: "Send views to a TikTok video (Admin only)",
			DefaultMemberPermissions: &[]int64{discordgo.PermissionAdministrator}[0],
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "id",
					Description: "TikTok video ID or URL",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "amount",
					Description: "Number of views to send",
					Required:    true,
					MinValue:    float64Ptr(1),
					MaxValue:    100000,
				},
			},
		},
		{
			Name:        "stats",
			Description: "View total views sent (auto-refreshes every minute)",
			DefaultMemberPermissions: &[]int64{0}[0],
		},
		{
			Name:        "resetstats",
			Description: "Reset all statistics (Admin only)",
			DefaultMemberPermissions: &[]int64{discordgo.PermissionAdministrator}[0],
		},
		{
			Name:        "50",
			Description: "50/50 chance - will it be Yes or No?",
			DefaultMemberPermissions: &[]int64{0}[0],
		},
	}

	for _, cmd := range commands {
		_, err = dg.ApplicationCommandCreate(dg.State.User.ID, "", cmd)
		if err != nil {
			fmt.Printf("Error creating command %s: %v\n", cmd.Name, err)
		}
	}

	<-make(chan struct{})
}

func float64Ptr(f float64) *float64 {
	return &f
}

func readyHandler(s *discordgo.Session, r *discordgo.Ready) {
	fmt.Printf("Logged in as: %v#%v\n", s.State.User.Username, s.State.User.Discriminator)
}

func interactionCreateHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()

	switch data.Name {
	case "views":
		handleViewsCommand(s, i, data)
	case "stats":
		handleStatsCommand(s, i)
	case "resetstats":
		handleResetStatsCommand(s, i)
	case "50":
		handleFiftyCommand(s, i)
	}
}

func handleViewsCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	// Check if user has admin permissions
	perms, err := s.UserChannelPermissions(i.Member.User.ID, i.ChannelID)
	if err != nil || perms&discordgo.PermissionAdministrator == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ You need **Administrator** permissions to use this command!",
			},
		})
		return
	}

	// Defer response
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	// Get parameters
	var vidStr string
	var amount int

	for _, opt := range data.Options {
		switch opt.Name {
		case "id":
			vidStr = opt.StringValue()
		case "amount":
			amount = int(opt.IntValue())
		}
	}

	// Extract video ID
	vid, err := getvid(vidStr)
	if err != nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf("❌ Error: %v", err)),
		})
		return
	}

	// Send initial status
	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: stringPtr(fmt.Sprintf("📤 Sending **%d** views to video ID: `%s`\n⏳ Progress will update every 5 seconds...", amount, vid)),
	})

	// Create a channel for progress updates
	updateChan := make(chan int64, 100)
	doneChan := make(chan struct{})

	// Start a goroutine to handle progress updates
	go func() {
		defer close(doneChan)
		
		// Send initial update
		total, _, _, _, _ := stats.GetStats()
		updateChan <- total
		
		// Start the view bot
		sent := runTikTok(vid, amount, updateChan)

		// Get updated stats
		total, _, _, uptime, daily := stats.GetStats()

		// Send completion message with stats
		message := fmt.Sprintf(
			"✅ Successfully sent **%d** views to video ID: `%s`\n\n"+
			"📊 **Final Statistics:**\n"+
			"• Views today: **%s**\n"+
			"• Total views: **%d**\n"+
			"• Uptime: **%s**\n"+
			"• This session: **%d** views",
			sent, vid, daily, total, formatDuration(uptime), sent,
		)

		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(message),
		})
	}()

	// Handle progress updates
	go func() {
		for count := range updateChan {
			total, _, _, _, daily := stats.GetStats()
			message := fmt.Sprintf(
				"📤 Sending **%d** views to video ID: `%s`\n"+
				"⏳ **Current Progress:**\n"+
				"• Sent so far: **%d**\n"+
				"• Views today: **%s**\n"+
				"• Total views: **%d**\n"+
				"⏰ Updates every 5 seconds...",
				amount, vid, count, daily, total,
			)
			
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: stringPtr(message),
			})
		}
	}()
}

func handleStatsCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	total, _, _, uptime, daily := stats.GetStats()

	message := fmt.Sprintf(
		"📊 **TikTok View Bot Statistics**\n"+
		"═══════════════════════════\n"+
		"📈 Views today: **%s**\n"+
		"📊 Total views: **%d**\n"+
		"⏱️ Uptime: **%s**\n"+
		"🔄 Last reset: **%s**\n"+
		"⏰ **Auto-refreshes every minute**\n"+
		"🕐 Last updated: **%s**",
		daily, total, formatDuration(uptime),
		stats.lastReset.Format("2006-01-02 15:04:05"),
		time.Now().Format("15:04:05"),
	)

	// Respond with the message
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
		},
	})

	if err != nil {
		fmt.Println("Error responding to interaction:", err)
		return
	}

	// Get the message and store it for auto-refresh
	go func() {
		// Wait for the message to be sent
		time.Sleep(2 * time.Second)
		
		// Get the interaction response
		webhook, err := s.InteractionResponse(i.Interaction)
		if err != nil {
			fmt.Println("Error getting interaction response:", err)
			return
		}
		
		if webhook != nil && webhook.ID != "" {
			// Store the message for auto-refresh
			statsMutex.Lock()
			activeStats[webhook.ID] = &ActiveStatMessage{
				ChannelID: i.ChannelID,
				MessageID: webhook.ID,
				Session:   s,
			}
			statsMutex.Unlock()
			fmt.Printf("✅ Stats message registered for auto-refresh: %s\n", webhook.ID)
			
			// Immediately update stats
			updateAllStats()
		}
	}()
}

func handleResetStatsCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Check if user has admin permissions
	perms, err := s.UserChannelPermissions(i.Member.User.ID, i.ChannelID)
	if err != nil || perms&discordgo.PermissionAdministrator == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ You need **Administrator** permissions to use this command!",
			},
		})
		return
	}

	stats.Reset()
	
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "✅ Statistics have been reset successfully!",
		},
	})
	
	// Update all stats messages
	updateAllStats()
}

func handleFiftyCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Generate random number between 1-100
	// Yes: 1-65 (65% chance)
	// No: 66-100 (35% chance)
	randomNum := rand.Intn(100) + 1
	
	var result string
	var emoji string
	
	if randomNum <= 65 {
		result = "YES"
		emoji = "✅"
	} else {
		result = "NO"
		emoji = "❌"
	}
	
	message := fmt.Sprintf(
		"🎲 **50 / 50 CHANCE**\n"+
		"═══════════════════════\n"+
		"%s The result is... **%s**!\n"+
		"\n"+
		"📊 **Stats:**\n"+
		"• Rolled: **%d**/100\n"+
		"• Actual odds: **65%%** YES / **35%%** NO",
		emoji, result, randomNum,
	)
	
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
		},
	})
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm %ds", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func stringPtr(s string) *string {
	return &s
}
