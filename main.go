package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jdkato/prose/v2"
	"github.com/sqweek/dialog"
	"gopkg.in/yaml.v2"
)

// Configuration structure for output options
type OutputConfig struct {
	IncludePhonetic bool `yaml:"includePhonetic"`
	IncludeOrigin   bool `yaml:"includeOrigin"`
	IncludeSynonyms bool `yaml:"includeSynonyms"`
	IncludeAntonyms bool `yaml:"includeAntonyms"`
	FilterNoExample bool `yaml:"filterDefinitionsWithoutExamples"`
}

// Proxy configuration structure
type ProxyConfig struct {
	HTTPProxy  string `yaml:"httpProxy"`
	HTTPSProxy string `yaml:"httpsProxy"`
}

// Cache structure to store API responses
type WordCache struct {
	Definitions []Definition
	Phonetic    string
	Origin      string
	Synonyms    []string
	Antonyms    []string
}

// Definition structure to store word definitions
type Definition struct {
	PartOfSpeech string
	Definition   string
	Example      string
	Synonyms     []string
	Antonyms     []string
}

// Global variables for configuration and cache
var config OutputConfig
var proxyConfig ProxyConfig
var wordCache map[string]WordCache = make(map[string]WordCache)
var cachePath string = "word_cache.json"
var logFile *os.File

// Helper function to verify English text validity
func isEnglishText(text string) bool {
	for _, r := range text {
		if !unicode.IsLetter(r) && r != ' ' && r != '-' && r != '/' {
			return false
		}
		if !unicode.In(r, unicode.Latin) {
			return false
		}
	}
	return true
}

// Capitalize the first letter of each word
func capitalizePhrase(phrase string) string {
	words := strings.Fields(phrase)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}
	return strings.Join(words, " ")
}

// Split slash-separated words into individual words
func splitSlashSeparatedWords(text string) []string {
	parts := strings.Split(text, "/")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}

// Count frequencies of items in a list
func countFrequencies(content []string) map[string]int {
	counts := make(map[string]int)
	for _, item := range content {
		capitalizedItem := capitalizePhrase(item)
		counts[capitalizedItem]++
	}
	return counts
}

// Sort items by frequency (descending order)
func sortByFrequency(counts map[string]int) []string {
	type itemFrequency struct {
		Item      string
		Frequency int
	}
	var items []itemFrequency
	for item, freq := range counts {
		items = append(items, itemFrequency{Item: item, Frequency: freq})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Frequency > items[j].Frequency
	})
	var sortedItems []string
	for _, entry := range items {
		sortedItems = append(sortedItems, entry.Item)
	}
	return sortedItems
}

// Load configuration from YAML file
func loadConfig() OutputConfig {
	defaultConfig := OutputConfig{
		IncludePhonetic: false,
		IncludeOrigin:   false,
		IncludeSynonyms: false,
		IncludeAntonyms: false,
		FilterNoExample: false,
	}

	// Check if config file exists
	configPath := "outputConfig.yml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config file if it doesn't exist
		yamlData, err := yaml.Marshal(defaultConfig)
		if err != nil {
			log.Println("Error creating default config:", err)
			return defaultConfig
		}
		err = ioutil.WriteFile(configPath, yamlData, 0644)
		if err != nil {
			log.Println("Error writing default config file:", err)
		}
		return defaultConfig
	}

	// Read and parse config file
	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Printf("Error reading config file: %v. Using defaults.\n", err)
		return defaultConfig
	}

	var config OutputConfig
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		log.Printf("Error parsing config file: %v. Using defaults.\n", err)
		return defaultConfig
	}

	return config
}

// Load proxy configuration from YAML file
func loadProxyConfig() ProxyConfig {
	defaultConfig := ProxyConfig{
		HTTPProxy:  "",
		HTTPSProxy: "",
	}

	// Check if config file exists
	configPath := "proxy.yml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config file if it doesn't exist
		yamlData, err := yaml.Marshal(defaultConfig)
		if err != nil {
			log.Println("Error creating default proxy config:", err)
			return defaultConfig
		}
		err = ioutil.WriteFile(configPath, yamlData, 0644)
		if err != nil {
			log.Println("Error writing default proxy config file:", err)
		}
		log.Println("Created default proxy.yml file. Please configure your proxy settings there if needed.")
		return defaultConfig
	}

	// Read and parse config file
	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Printf("Error reading proxy config file: %v. Using no proxy.\n", err)
		return defaultConfig
	}

	var config ProxyConfig
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		log.Printf("Error parsing proxy config file: %v. Using no proxy.\n", err)
		return defaultConfig
	}

	if config.HTTPProxy != "" || config.HTTPSProxy != "" {
		log.Println("Proxy configuration loaded successfully.")
		log.Printf("HTTP Proxy: %s\n", config.HTTPProxy)
		log.Printf("HTTPS Proxy: %s\n", config.HTTPSProxy)
	} else {
		log.Println("No proxy configuration found. API requests will be made directly.")
	}

	return config
}

// Load word cache from file
func loadWordCache() {
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		return // No cache file exists yet
	}

	data, err := ioutil.ReadFile(cachePath)
	if err != nil {
		log.Println("Error reading cache file:", err)
		return
	}

	err = json.Unmarshal(data, &wordCache)
	if err != nil {
		log.Println("Error parsing cache file:", err)
		wordCache = make(map[string]WordCache)
	}
}

// Save word cache to file
func saveWordCache() {
	data, err := json.MarshalIndent(wordCache, "", "  ")
	if err != nil {
		log.Println("Error serializing cache:", err)
		return
	}

	err = ioutil.WriteFile(cachePath, data, 0644)
	if err != nil {
		log.Println("Error writing cache file:", err)
	}
}

// Check if word exists in cache
func checkCache(word string) (WordCache, bool) {
	cached, exists := wordCache[strings.ToLower(word)]
	return cached, exists
}

// Format a list of words for output
func formatWordList(words []string) string {
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, ", ")
}

// Create an HTTP client with proxy support if configured
func createHTTPClient() (*http.Client, error) {
	transport := &http.Transport{}

	// Check if we have proxy configuration
	if proxyConfig.HTTPSProxy != "" {
		proxyURL, err := url.Parse(proxyConfig.HTTPSProxy)
		if err != nil {
			log.Printf("Error parsing HTTPS proxy URL: %v. Will try without proxy.\n", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
			log.Printf("Using HTTPS proxy: %s\n", proxyConfig.HTTPSProxy)
		}
	} else if proxyConfig.HTTPProxy != "" {
		proxyURL, err := url.Parse(proxyConfig.HTTPProxy)
		if err != nil {
			log.Printf("Error parsing HTTP proxy URL: %v. Will try without proxy.\n", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
			log.Printf("Using HTTP proxy: %s\n", proxyConfig.HTTPProxy)
		}
	}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}, nil
}

// Fetch word explanations using the Free Dictionary API
func fetchWordDetails(word string) string {
	// Check cache first
	cachedData, exists := checkCache(word)
	if !exists {
		// Fetch from API if not in cache
		url := fmt.Sprintf("https://api.dictionaryapi.dev/api/v2/entries/en/%s", strings.ToLower(word))
		log.Printf("Fetching word '%s' from URL: %s\n", word, url)

		// Create a new request with proper headers
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Printf("Error creating request for %s: %v\n", word, err)
			return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
		}

		// Add headers that might help with the request
		req.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
		req.Header.Add("Accept", "application/json")

		// Create HTTP client with proxy support if configured
		client, err := createHTTPClient()
		if err != nil {
			log.Printf("Error creating HTTP client: %v. Will use default client.\n", err)
			client = &http.Client{Timeout: 10 * time.Second}
		}

		// Send the request
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Network error when fetching %s: %v\n", word, err)
			return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
		}
		defer resp.Body.Close()

		// Read the full response body for debugging
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading response body for %s: %v\n", word, err)
			return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
		}

		// Log response details
		log.Printf("Response status for '%s': %s\n", word, resp.Status)
		log.Printf("Response body for '%s': %s\n", word, string(bodyBytes))

		// Check if response status code is successful (200 OK)
		if resp.StatusCode != http.StatusOK {
			log.Printf("API returned non-OK status code %d for word %s\n", resp.StatusCode, word)
			return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
		}

		// Create a new reader with the body bytes for JSON decoding
		var result []map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			log.Printf("Error parsing JSON for %s: %v\n", word, err)
			return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
		}

		// Check if the result is empty or doesn't contain expected data
		if len(result) == 0 {
			log.Printf("Empty result array for word %s\n", word)
			return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
		}

		// Process API response into our cache structure
		cachedData = WordCache{
			Definitions: []Definition{},
			Phonetic:    "",
			Origin:      "",
			Synonyms:    []string{},
			Antonyms:    []string{},
		}

		// Extract phonetic if available
		if phonetic, ok := result[0]["phonetic"].(string); ok {
			cachedData.Phonetic = phonetic
		}

		// Try to find phonetics array if direct phonetic field is empty
		if cachedData.Phonetic == "" {
			if phonetics, ok := result[0]["phonetics"].([]interface{}); ok && len(phonetics) > 0 {
				for _, p := range phonetics {
					if phoneticMap, ok := p.(map[string]interface{}); ok {
						if text, ok := phoneticMap["text"].(string); ok && text != "" {
							cachedData.Phonetic = text
							break
						}
					}
				}
			}
		}

		// Extract origin if available
		if origin, ok := result[0]["origin"].(string); ok {
			cachedData.Origin = origin
		}

		// Process all definitions
		for _, entry := range result {
			if meanings, ok := entry["meanings"].([]interface{}); ok {
				for _, meaning := range meanings {
					meaningMap, ok := meaning.(map[string]interface{})
					if !ok {
						log.Printf("Warning: meaning is not a map for word %s\n", word)
						continue
					}

					partOfSpeech, ok := meaningMap["partOfSpeech"].(string)
					if !ok {
						log.Printf("Warning: partOfSpeech not found for word %s\n", word)
						continue
					}

					// Extract synonyms and antonyms at the meaning level
					var meaningLevelSynonyms, meaningLevelAntonyms []string
					if syns, ok := meaningMap["synonyms"].([]interface{}); ok {
						for _, syn := range syns {
							if synStr, ok := syn.(string); ok {
								meaningLevelSynonyms = append(meaningLevelSynonyms, synStr)
								cachedData.Synonyms = append(cachedData.Synonyms, synStr)
							}
						}
					}

					if ants, ok := meaningMap["antonyms"].([]interface{}); ok {
						for _, ant := range ants {
							if antStr, ok := ant.(string); ok {
								meaningLevelAntonyms = append(meaningLevelAntonyms, antStr)
								cachedData.Antonyms = append(cachedData.Antonyms, antStr)
							}
						}
					}

					// Process definitions
					if definitions, ok := meaningMap["definitions"].([]interface{}); ok {
						for _, def := range definitions {
							defMap, ok := def.(map[string]interface{})
							if !ok {
								continue
							}

							definitionText, ok := defMap["definition"].(string)
							if !ok {
								continue
							}

							// Extract example if available
							example := ""
							if ex, ok := defMap["example"].(string); ok {
								example = ex
							}

							// Extract definition level synonyms and antonyms
							var defSynonyms, defAntonyms []string
							if syns, ok := defMap["synonyms"].([]interface{}); ok {
								for _, syn := range syns {
									if synStr, ok := syn.(string); ok {
										defSynonyms = append(defSynonyms, synStr)
										cachedData.Synonyms = append(cachedData.Synonyms, synStr)
									}
								}
							}

							if ants, ok := defMap["antonyms"].([]interface{}); ok {
								for _, ant := range ants {
									if antStr, ok := ant.(string); ok {
										defAntonyms = append(defAntonyms, antStr)
										cachedData.Antonyms = append(cachedData.Antonyms, antStr)
									}
								}
							}

							// Add definition with combined synonyms and antonyms
							allSyns := append(defSynonyms, meaningLevelSynonyms...)
							allAnts := append(defAntonyms, meaningLevelAntonyms...)

							cachedData.Definitions = append(cachedData.Definitions, Definition{
								PartOfSpeech: partOfSpeech,
								Definition:   definitionText,
								Example:      example,
								Synonyms:     allSyns,
								Antonyms:     allAnts,
							})
						}
					}
				}
			}
		}

		// Add to cache for future use
		wordCache[strings.ToLower(word)] = cachedData
		// Save cache after each new word
		saveWordCache()
	}

	// Format the word details based on cached data and configuration
	var details strings.Builder

	// Add word name and phonetic (if enabled)
	capitalized := capitalizePhrase(word)
	if config.IncludePhonetic && cachedData.Phonetic != "" {
		details.WriteString(fmt.Sprintf("%s %s\n", capitalized, cachedData.Phonetic))
	} else {
		details.WriteString(capitalized + "\n")
	}

	// Add origin if enabled
	if config.IncludeOrigin && cachedData.Origin != "" {
		details.WriteString(fmt.Sprintf("\t%s Origin: %s\n", capitalized, cachedData.Origin))
	}

	// If no definitions available, return basic info
	if len(cachedData.Definitions) == 0 {
		details.WriteString("\tNo details available.\n")
		return details.String()
	}

	// Process definitions
	for i, def := range cachedData.Definitions {
		// Skip definitions without examples if filtering is enabled
		if config.FilterNoExample && def.Example == "" {
			continue
		}

		// Write definition with number
		details.WriteString(fmt.Sprintf("\t%s %d, %s: %s\n", capitalized, i+1, def.PartOfSpeech, def.Definition))

		// Add example if available
		if def.Example != "" {
			details.WriteString(fmt.Sprintf("\t\t%s %d Example: %s\n", capitalized, i+1, def.Example))
		}

		// Add synonyms if enabled and available
		if config.IncludeSynonyms && len(def.Synonyms) > 0 {
			details.WriteString(fmt.Sprintf("\t\t%s %d Synonyms: %s\n", capitalized, i+1, formatWordList(def.Synonyms)))
		}

		// Add antonyms if enabled and available
		if config.IncludeAntonyms && len(def.Antonyms) > 0 {
			details.WriteString(fmt.Sprintf("\t\t%s %d Antonyms: %s\n", capitalized, i+1, formatWordList(def.Antonyms)))
		}
	}

	return details.String()
}

// Sets up logging to write to a log file
func setupLogging() {
	// Create or open log file
	var err error
	logFile, err = os.OpenFile("log.txt", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println("Failed to open log file:", err)
		return
	}

	// Configure log package to write to the file
	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags)
}

// Prints dynamic progress monitoring info with clear formatting
func printProgress(stage string, item string, current, total int) {
	percentage := int((float64(current) / float64(total)) * 100)
	// Log progress to file
	log.Printf("%s: %s (%d of %d) - %d%% Complete", stage, capitalizePhrase(item), current, total, percentage)

	// Clear the entire line before printing new progress
	fmt.Printf("\r%-80s", " ") // Clear previous content with spaces
	fmt.Printf("\r%s: %s (%d of %d) - %d%% Complete", stage, capitalizePhrase(item), current, total, percentage)
}

// Categorizes text based on linguistic features
func categorizeText(inputFile string) error {
	baseFileName := strings.TrimSuffix(filepath.Base(inputFile), filepath.Ext(inputFile))
	outputDir := baseFileName

	// Create the output directory
	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// Open the input file
	file, err := os.Open(inputFile)
	if err != nil {
		return fmt.Errorf("failed to open input file: %v", err)
	}
	defer file.Close()

	// Read file content
	scanner := bufio.NewScanner(file)
	var content string
	for scanner.Scan() {
		content += scanner.Text() + " "
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading input file: %v", err)
	}

	// Create NLP document
	doc, err := prose.NewDocument(content)
	if err != nil {
		return fmt.Errorf("error creating Prose document: %v", err)
	}

	// Define output files
	categoryFiles := map[string]string{
		"Nouns":      baseFileName + "_Nouns.txt",
		"Verbs":      baseFileName + "_Verbs.txt",
		"Adjectives": baseFileName + "_Adjectives.txt",
		"Adverbs":    baseFileName + "_Adverbs.txt",
		"OtherWords": baseFileName + "_OtherWords.txt",
	}

	explanationFiles := map[string]string{}
	for category, file := range categoryFiles {
		explanationFiles[category] = strings.Replace(file, ".txt", "_ex.txt", 1)
	}

	categorizedContent := map[string][]string{}
	allWords := map[string]int{}

	// Process tokens for classification
	tokens := doc.Tokens()
	totalTokens := len(tokens)
	log.Println("Starting text classification...")

	for i, tok := range tokens {
		text := strings.ToLower(tok.Text)
		printProgress("Classifying text", text, i+1, totalTokens)

		// Process slash-separated words
		wordParts := splitSlashSeparatedWords(text)
		for _, part := range wordParts {
			if isEnglishText(part) {
				allWords[part]++
				var category string
				switch tok.Tag {
				case "NN", "NNS", "NNP", "NNPS":
					category = "Nouns"
				case "VB", "VBD", "VBP", "VBZ", "VBG":
					category = "Verbs"
				case "JJ", "JJR", "JJS":
					category = "Adjectives"
				case "RB", "RBR", "RBS":
					category = "Adverbs"
				default:
					category = "OtherWords"
				}
				categorizedContent[category] = append(categorizedContent[category], part)
			}
		}
	}

	log.Println("\nClassification complete. Starting dictionary lookups...")
	fmt.Println("\nClassification complete. Starting dictionary lookups...")

	// Get all unique words for total word count display
	sortedAllWords := sortByFrequency(allWords)
	totalUniqueWords := len(sortedAllWords)

	// Track progress across all words being processed
	wordCounter := 0
	totalWordsToProcess := 0
	for _, words := range categorizedContent {
		totalWordsToProcess += len(countFrequencies(words)) // Count unique words per category
	}

	// Write categorized content to individual files
	for category, words := range categorizedContent {
		filePath := filepath.Join(outputDir, categoryFiles[category])
		exFilePath := filepath.Join(outputDir, explanationFiles[category])

		wordFile, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("failed to create output file for %s: %v", category, err)
		}
		defer wordFile.Close()

		exFile, err := os.Create(exFilePath)
		if err != nil {
			return fmt.Errorf("failed to create explanation file for %s: %v", category, err)
		}
		defer exFile.Close()

		wordWriter := bufio.NewWriter(wordFile)
		exWriter := bufio.NewWriter(exFile)

		sortedWords := sortByFrequency(countFrequencies(words))

		log.Printf("\nProcessing %s category (%d words):\n", category, len(sortedWords))
		fmt.Printf("\nProcessing %s category (%d words):\n", category, len(sortedWords))

		for i, word := range sortedWords {
			wordWriter.WriteString(capitalizePhrase(word) + "\n")
			wordCounter++
			printProgress(
				fmt.Sprintf("Dictionary lookup (%s)", category),
				word,
				i+1,
				len(sortedWords))
			exWriter.WriteString(fetchWordDetails(word) + "\n")
		}
		wordWriter.Flush()
		exWriter.Flush()
		log.Printf("\n- Category '%s' processed: %d words\n", category, len(sortedWords))
		fmt.Printf("\n- Category '%s' processed: %d words\n", category, len(sortedWords))
	}

	log.Println("\nGenerating final outputs...")
	fmt.Println("\nGenerating final outputs...")

	// Write `_AllWords_ex.txt` file
	allWordsExFilePath := filepath.Join(outputDir, baseFileName+"_AllWords_ex.txt")
	allWordsExFile, err := os.Create(allWordsExFilePath)
	if err != nil {
		return fmt.Errorf("failed to create _AllWords_ex.txt file: %v", err)
	}
	defer allWordsExFile.Close()

	allWordsExWriter := bufio.NewWriter(allWordsExFile)
	for i, word := range sortedAllWords {
		printProgress("Processing All Words explanations", word, i+1, totalUniqueWords)
		allWordsExWriter.WriteString(fetchWordDetails(word) + "\n")
	}
	allWordsExWriter.Flush()
	log.Println("\n- AllWords_ex.txt complete")
	fmt.Println("\n- AllWords_ex.txt complete")

	// Write `_AllWords.txt` file
	allWordsFilePath := filepath.Join(outputDir, baseFileName+"_AllWords.txt")
	allWordsFile, err := os.Create(allWordsFilePath)
	if err != nil {
		return fmt.Errorf("failed to create _AllWords.txt file: %v", err)
	}
	defer allWordsFile.Close()

	allWordsWriter := bufio.NewWriter(allWordsFile)
	for _, word := range sortedAllWords {
		allWordsWriter.WriteString(capitalizePhrase(word) + "\n")
	}
	allWordsWriter.Flush()
	log.Println("- AllWords.txt complete")
	fmt.Println("- AllWords.txt complete")

	// Report results
	log.Printf("\n===== Analysis Results =====\n")
	log.Printf("Total unique words after deduplication: %d\n", totalUniqueWords)
	log.Printf("Results written to directory: %s\n", outputDir)

	fmt.Printf("\n===== Analysis Results =====\n")
	fmt.Printf("Total unique words after deduplication: %d\n", totalUniqueWords)
	fmt.Printf("Results written to directory: %s\n", outputDir)
	log.Println("Text analysis complete.")

	return nil
}

func main() {
	// Setup logging
	setupLogging()
	defer logFile.Close()

	log.Println("Application started")

	// Load configuration and proxy settings
	config = loadConfig()
	proxyConfig = loadProxyConfig()
	loadWordCache()

	log.Println("Select the input text file:")
	fmt.Println("Select the input text file:")
	inputFile, err := dialog.File().Title("Select Input File").Filter("Text Files (*.txt)", "txt").Load()
	if err != nil || inputFile == "" {
		log.Println("No file selected or error occurred.")
		fmt.Println("No file selected or error occurred.")
		return
	}

	err = categorizeText(inputFile)
	if err != nil {
		log.Println("Error during categorization:", err)
		fmt.Println("Error during categorization:", err)
		return
	}

	log.Println("Text analysis complete.")
	fmt.Println("Text analysis complete.")
}
