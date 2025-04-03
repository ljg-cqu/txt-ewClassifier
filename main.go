package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
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

// Configuration structures
type InputConfig struct {
	FilePath string `yaml:"filePath"`
}

type OutputConfig struct {
	IncludePhonetic          bool `yaml:"includePhonetic"`
	IncludeOrigin            bool `yaml:"includeOrigin"`
	IncludeSynonyms          bool `yaml:"includeSynonyms"`
	IncludeAntonyms          bool `yaml:"includeAntonyms"`
	FilterNoExample          bool `yaml:"filterDefinitionsWithoutExamples"`
	GenerateExplanations     bool `yaml:"generateExplanations"`     // Toggle for explanation files
	GenerateExampleSentences bool `yaml:"generateExampleSentences"` // Toggle for example sentences files
	MaxExampleSentences      int  `yaml:"maxExampleSentences"`      // Maximum number of example sentences per word
}

type QueryConfig struct {
	QueryForUnknownWords bool `yaml:"queryForUnknownWords"` // Whether to query unknown words
}

type ProxyConfig struct {
	HTTPProxy  string `yaml:"httpProxy"`
	HTTPSProxy string `yaml:"httpsProxy"`
}

type Definition struct {
	PartOfSpeech string
	Definition   string
	Example      string
	Synonyms     []string
	Antonyms     []string
}

type WordCache struct {
	Definitions []Definition
	Phonetic    string
	Origin      string
	Synonyms    []string
	Antonyms    []string
}

// Global variables
var config OutputConfig
var queryConfig QueryConfig
var proxyConfig ProxyConfig
var wordCache = make(map[string]WordCache)
var wordUnknown = make(map[string]bool)
var cachePath = "word_cache.json"
var unknownPath = "word_unknown.json"
var logFile *os.File

// Helper functions
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

func capitalizePhrase(phrase string) string {
	words := strings.Fields(phrase)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}
	return strings.Join(words, " ")
}

func capitalizeSentence(sentence string) string {
	if len(sentence) == 0 {
		return ""
	}
	return strings.ToUpper(string(sentence[0])) + sentence[1:]
}

func splitSlashSeparatedWords(text string) []string {
	parts := strings.Split(text, "/")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}

func countFrequencies(content []string) map[string]int {
	counts := make(map[string]int)
	for _, item := range content {
		counts[capitalizePhrase(item)]++
	}
	return counts
}

func sortByFrequency(counts map[string]int) []string {
	type itemFreq struct {
		Item string
		Freq int
	}
	var items []itemFreq
	for item, freq := range counts {
		items = append(items, itemFreq{Item: item, Freq: freq})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Freq > items[j].Freq
	})
	var result []string
	for _, item := range items {
		result = append(result, item.Item)
	}
	return result
}

// Configuration loading
func loadInputConfig() InputConfig {
	defaultConfig := InputConfig{
		FilePath: "", // Default to empty string (will trigger GUI selection)
	}

	configPath := "inputConfig.yml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config file if it doesn't exist
		yamlData, _ := yaml.Marshal(defaultConfig)
		ioutil.WriteFile(configPath, yamlData, 0644)
		return defaultConfig
	}

	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		return defaultConfig
	}

	var config InputConfig
	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return defaultConfig
	}
	return config
}

func loadConfig() OutputConfig {
	defaultConfig := OutputConfig{
		IncludePhonetic:          false,
		IncludeOrigin:            false,
		IncludeSynonyms:          false,
		IncludeAntonyms:          false,
		FilterNoExample:          false,
		GenerateExplanations:     true, // Default to true for backward compatibility
		GenerateExampleSentences: true, // Default to true for example sentences files
		MaxExampleSentences:      0,    // Default to 0 (no limit)
	}

	configPath := "outputConfig.yml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		yamlData, _ := yaml.Marshal(defaultConfig)
		ioutil.WriteFile(configPath, yamlData, 0644)
		return defaultConfig
	}

	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		return defaultConfig
	}

	var config OutputConfig
	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return defaultConfig
	}
	return config
}

func loadQueryConfig() QueryConfig {
	defaultConfig := QueryConfig{
		QueryForUnknownWords: false, // Default: don't query unknown words
	}

	configPath := "queryConfig.yml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		yamlData, _ := yaml.Marshal(defaultConfig)
		ioutil.WriteFile(configPath, yamlData, 0644)
		return defaultConfig
	}

	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		return defaultConfig
	}

	var config QueryConfig
	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return defaultConfig
	}
	return config
}

func loadProxyConfig() ProxyConfig {
	defaultConfig := ProxyConfig{
		HTTPProxy:  "",
		HTTPSProxy: "",
	}

	configPath := "proxy.yml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		yamlData, _ := yaml.Marshal(defaultConfig)
		ioutil.WriteFile(configPath, yamlData, 0644)
		return defaultConfig
	}

	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		return defaultConfig
	}

	var config ProxyConfig
	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return defaultConfig
	}
	return config
}

// Cache management
func loadWordCache() {
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		return
	}

	data, err := ioutil.ReadFile(cachePath)
	if err != nil {
		return
	}

	if err := json.Unmarshal(data, &wordCache); err != nil {
		wordCache = make(map[string]WordCache)
	}
}

func saveWordCache() {
	data, err := json.MarshalIndent(wordCache, "", "  ")
	if err != nil {
		return
	}
	ioutil.WriteFile(cachePath, data, 0644)
}

func loadWordUnknown() {
	if _, err := os.Stat(unknownPath); os.IsNotExist(err) {
		return
	}

	data, err := ioutil.ReadFile(unknownPath)
	if err != nil {
		return
	}

	if err := json.Unmarshal(data, &wordUnknown); err != nil {
		wordUnknown = make(map[string]bool)
	}
}

func saveWordUnknown() {
	data, err := json.MarshalIndent(wordUnknown, "", "  ")
	if err != nil {
		return
	}
	ioutil.WriteFile(unknownPath, data, 0644)
}

func createHTTPClient() *http.Client {
	transport := &http.Transport{}

	if proxyConfig.HTTPSProxy != "" {
		proxyURL, err := url.Parse(proxyConfig.HTTPSProxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	} else if proxyConfig.HTTPProxy != "" {
		proxyURL, err := url.Parse(proxyConfig.HTTPProxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
}

// Check if a word has details available, returns true if it has details, false if not
func hasWordDetails(word string) bool {
	word = strings.ToLower(word)

	// Check if the word is in the cache
	cachedData, exists := wordCache[word]
	if exists && len(cachedData.Definitions) > 0 {
		return true
	}

	return false
}

// Query the dictionary API for a word
func queryDictionaryAPI(word string) bool {
	apiURL := fmt.Sprintf("https://api.dictionaryapi.dev/api/v2/entries/en/%s", word)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return false
	}

	req.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Add("Accept", "application/json")

	client := createHTTPClient()
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return false
	}
	defer resp.Body.Close()

	bodyBytes, _ := ioutil.ReadAll(resp.Body)

	var result []map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil || len(result) == 0 {
		return false
	}

	// Process API response into cache structure
	cachedData := WordCache{
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

	// Extract phonetics
	if phonetics, ok := result[0]["phonetics"].([]interface{}); ok && cachedData.Phonetic == "" {
		for _, p := range phonetics {
			if phoneticMap, ok := p.(map[string]interface{}); ok {
				if text, ok := phoneticMap["text"].(string); ok && text != "" {
					cachedData.Phonetic = text
					break
				}
			}
		}
	}

	// Extract origin directly from the top level
	if originStr, ok := result[0]["origin"].(string); ok {
		cachedData.Origin = originStr
	}

	// Extract meanings, definitions, synonyms, antonyms
	if meanings, ok := result[0]["meanings"].([]interface{}); ok {
		for _, m := range meanings {
			if meaningMap, ok := m.(map[string]interface{}); ok {
				partOfSpeech := ""
				if pos, ok := meaningMap["partOfSpeech"].(string); ok {
					partOfSpeech = pos
				}

				// Extract definitions
				if definitions, ok := meaningMap["definitions"].([]interface{}); ok {
					for _, d := range definitions {
						defMap, ok := d.(map[string]interface{})
						if !ok {
							continue
						}

						def := Definition{
							PartOfSpeech: partOfSpeech,
							Definition:   "",
							Example:      "",
							Synonyms:     []string{},
							Antonyms:     []string{},
						}

						if defStr, ok := defMap["definition"].(string); ok {
							def.Definition = defStr
						}

						if exampleStr, ok := defMap["example"].(string); ok {
							def.Example = exampleStr
						}

						// Extract synonyms and antonyms
						if syns, ok := defMap["synonyms"].([]interface{}); ok {
							for _, syn := range syns {
								if synStr, ok := syn.(string); ok {
									def.Synonyms = append(def.Synonyms, synStr)
									cachedData.Synonyms = append(cachedData.Synonyms, synStr)
								}
							}
						}

						if ants, ok := defMap["antonyms"].([]interface{}); ok {
							for _, ant := range ants {
								if antStr, ok := ant.(string); ok {
									def.Antonyms = append(def.Antonyms, antStr)
									cachedData.Antonyms = append(cachedData.Antonyms, antStr)
								}
							}
						}

						cachedData.Definitions = append(cachedData.Definitions, def)
					}
				}
			}
		}
	}

	// Save to cache if we found definitions
	if len(cachedData.Definitions) > 0 {
		wordCache[strings.ToLower(word)] = cachedData
		saveWordCache()
		return true
	}

	return false
}

// Fetches word details and returns formatted output (if available) or empty string for unknown words
func fetchWordDetails(word string) string {
	word = strings.ToLower(word)

	// Check if the word is in the unknown words database
	if _, isUnknown := wordUnknown[word]; isUnknown {
		// If configured not to query unknown words, return empty string
		if !queryConfig.QueryForUnknownWords {
			return ""
		}

		// Try to query API for this previously unknown word
		if !queryDictionaryAPI(word) {
			// Still unknown, return empty string
			return ""
		}

		// The word now has details, remove from unknown list
		delete(wordUnknown, word)
		saveWordUnknown()
	}

	// Check if the word is in the cache
	cachedData, exists := wordCache[word]

	// If not in cache, try to fetch from API
	if !exists {
		// Try to query API
		if !queryDictionaryAPI(word) {
			// Not found, add to unknown words and return empty
			wordUnknown[word] = true
			saveWordUnknown()
			return ""
		}

		// Now it should be in cache
		cachedData = wordCache[word]
	}

	// Format output with the layout
	var output strings.Builder
	capitalized := capitalizePhrase(word)

	// Put word and phonetic on the same line
	if cachedData.Phonetic != "" && config.IncludePhonetic {
		output.WriteString(fmt.Sprintf("%s %s\n", capitalized, cachedData.Phonetic))
	} else {
		output.WriteString(fmt.Sprintf("%s\n", capitalized))
	}

	// Add origin if available and enabled
	if config.IncludeOrigin && cachedData.Origin != "" {
		output.WriteString(fmt.Sprintf("\tOrigin: %s\n", cachedData.Origin))
	}

	// Check if there are definitions available
	if len(cachedData.Definitions) == 0 {
		// This shouldn't happen after our checks, but just in case
		wordUnknown[word] = true
		saveWordUnknown()
		return ""
	}

	// Process definitions with the new format
	for i, def := range cachedData.Definitions {
		if config.FilterNoExample && def.Example == "" {
			continue
		}

		defNumber := i + 1

		// Write definition with number and word prefix
		output.WriteString(fmt.Sprintf("\t%s %d, %s: %s\n",
			capitalized, defNumber, def.PartOfSpeech, def.Definition))

		// Add example if available, with word and number prefix
		if def.Example != "" {
			output.WriteString(fmt.Sprintf("\t\t%s %d Example: %s\n",
				capitalized, defNumber, def.Example))
		}

		// Add synonyms if enabled and available, with word and number prefix
		if config.IncludeSynonyms && len(def.Synonyms) > 0 {
			output.WriteString(fmt.Sprintf("\t\t%s %d Synonyms: %s\n",
				capitalized, defNumber, strings.Join(def.Synonyms, ", ")))
		}

		// Add antonyms if enabled and available, with word and number prefix
		if config.IncludeAntonyms && len(def.Antonyms) > 0 {
			output.WriteString(fmt.Sprintf("\t\t%s %d Antonyms: %s\n",
				capitalized, defNumber, strings.Join(def.Antonyms, ", ")))
		}
	}

	return strings.Trim(output.String(), "\n")
}

// Function to generate example sentences file for a word with the new selection logic
func generateExampleSentencesContent(word string) string {
	word = strings.ToLower(word)

	// Check if the word is unknown - if so, return empty string
	if _, isUnknown := wordUnknown[word]; isUnknown {
		return ""
	}

	cachedData, exists := wordCache[word]
	if !exists || len(cachedData.Definitions) == 0 {
		return ""
	}

	// Collect all example sentences for this word
	var exampleSentences []string
	for _, def := range cachedData.Definitions {
		if def.Example != "" {
			// Make sure the first letter is capitalized
			example := capitalizeSentence(def.Example)
			exampleSentences = append(exampleSentences, example)
		}
	}

	if len(exampleSentences) == 0 {
		return ""
	}

	// Apply the selection logic based on MaxExampleSentences setting
	var selectedExamples []string

	// If MaxExampleSentences is 0 (no limit) or greater than/equal to available examples,
	// use all available examples
	if config.MaxExampleSentences <= 0 || config.MaxExampleSentences >= len(exampleSentences) {
		selectedExamples = exampleSentences
	} else {
		// Need to randomly select MaxExampleSentences examples
		// Create a copy of exampleSentences to avoid modifying the original
		availableExamples := make([]string, len(exampleSentences))
		copy(availableExamples, exampleSentences)

		// Randomly select examples
		selectedExamples = make([]string, 0, config.MaxExampleSentences)
		for i := 0; i < config.MaxExampleSentences && len(availableExamples) > 0; i++ {
			// Pick a random index
			randIndex := rand.Intn(len(availableExamples))

			// Add the example at the random index to selected examples
			selectedExamples = append(selectedExamples, availableExamples[randIndex])

			// Remove the selected example to avoid duplicates
			availableExamples = append(availableExamples[:randIndex], availableExamples[randIndex+1:]...)
		}
	}

	// Format the output
	var output strings.Builder
	capitalized := capitalizePhrase(word)
	output.WriteString(capitalized)

	for _, example := range selectedExamples {
		output.WriteString("\n\t" + example)
	}

	return output.String()
}

func printProgress(stage string, item string, current, total int) {
	percentage := int((float64(current) / float64(total)) * 100)
	fmt.Printf("\r%-80s", " ") // Clear line
	fmt.Printf("\r%s: %s (%d of %d) - %d%%", stage, capitalizePhrase(item), current, total, percentage)
}

func setupLogging() {
	var err error
	logFile, err = os.OpenFile("log.txt", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(logFile)
		log.SetFlags(log.LstdFlags)
	}
}

func categorizeText(inputFile string) error {
	baseFileName := strings.TrimSuffix(filepath.Base(inputFile), filepath.Ext(inputFile))
	outputDir := baseFileName

	// Create output directory
	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		return err
	}

	// Read input file
	file, err := os.Open(inputFile)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var content string
	for scanner.Scan() {
		content += scanner.Text() + " "
	}

	// Create NLP document
	doc, err := prose.NewDocument(content)
	if err != nil {
		return err
	}

	// Define categories and files
	categories := map[string]string{
		"Nouns":      filepath.Join(outputDir, baseFileName+"_Nouns.txt"),
		"Verbs":      filepath.Join(outputDir, baseFileName+"_Verbs.txt"),
		"Adjectives": filepath.Join(outputDir, baseFileName+"_Adjectives.txt"),
		"Adverbs":    filepath.Join(outputDir, baseFileName+"_Adverbs.txt"),
		"OtherWords": filepath.Join(outputDir, baseFileName+"_OtherWords.txt"),
	}

	explanationFiles := map[string]string{}
	exampleSentencesFiles := map[string]string{}

	// Only create explanation file maps if the toggle is enabled
	if config.GenerateExplanations {
		for category, file := range categories {
			explanationFiles[category] = strings.Replace(file, ".txt", "_ex.txt", 1)
		}
	}

	// Only create example sentences file maps if the toggle is enabled
	if config.GenerateExampleSentences {
		for category, file := range categories {
			exampleSentencesFiles[category] = strings.Replace(file, ".txt", "_es.txt", 1)
		}
	}

	categorizedWords := map[string][]string{}
	allWords := map[string]int{}

	// Process tokens
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
				categorizedWords[category] = append(categorizedWords[category], part)
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
	for _, words := range categorizedWords {
		totalWordsToProcess += len(countFrequencies(words)) // Count unique words per category
	}

	// Map to track unknown words and their frequencies
	uniqueUnknownWords := make(map[string]int)

	// Write categorized content to individual files
	for category, words := range categorizedWords {
		filePath := categories[category]

		wordFile, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("failed to create output file for %s: %v", category, err)
		}
		defer wordFile.Close()

		wordWriter := bufio.NewWriter(wordFile)

		var exFile *os.File
		var exWriter *bufio.Writer

		var esFile *os.File
		var esWriter *bufio.Writer

		// Only create explanation files if the toggle is enabled
		if config.GenerateExplanations {
			exFilePath := explanationFiles[category]
			exFile, err = os.Create(exFilePath)
			if err != nil {
				return fmt.Errorf("failed to create explanation file for %s: %v", category, err)
			}
			defer exFile.Close()
			exWriter = bufio.NewWriter(exFile)
		}

		// Only create example sentences files if the toggle is enabled
		if config.GenerateExampleSentences {
			esFilePath := exampleSentencesFiles[category]
			esFile, err = os.Create(esFilePath)
			if err != nil {
				return fmt.Errorf("failed to create example sentences file for %s: %v", category, err)
			}
			defer esFile.Close()
			esWriter = bufio.NewWriter(esFile)
		}

		sortedWords := sortByFrequency(countFrequencies(words))

		log.Printf("\nProcessing %s category (%d words):\n", category, len(sortedWords))
		fmt.Printf("\nProcessing %s category (%d words):\n", category, len(sortedWords))

		// Track if we've written anything to the example sentences file
		hasWrittenExamples := false
		// Track if we've written anything to the explanation file
		hasWrittenExplanations := false

		for i, word := range sortedWords {
			wordCounter++
			printProgress(
				fmt.Sprintf("Dictionary lookup (%s)", category),
				word,
				i+1,
				len(sortedWords))

			// Fetch word details
			wordDetailsText := fetchWordDetails(word)

			// If word details are empty, the word is unknown
			if wordDetailsText == "" {
				// Track unknown words with their frequencies
				lowerWord := strings.ToLower(word)
				uniqueUnknownWords[lowerWord] += allWords[lowerWord]
				continue
			}

			// Word is known, add to regular output files
			wordWriter.WriteString(capitalizePhrase(word) + "\n")

			// Only write to explanation files if the toggle is enabled
			if config.GenerateExplanations {
				if hasWrittenExplanations {
					exWriter.WriteString("\n" + wordDetailsText)
				} else {
					exWriter.WriteString(wordDetailsText)
					hasWrittenExplanations = true
				}
			}

			// Only write to example sentences files if the toggle is enabled
			if config.GenerateExampleSentences {
				exampleContent := generateExampleSentencesContent(word)
				if exampleContent != "" {
					if hasWrittenExamples {
						esWriter.WriteString("\n" + exampleContent)
					} else {
						esWriter.WriteString(exampleContent)
						hasWrittenExamples = true
					}
				}
			}
		}

		wordWriter.Flush()
		if config.GenerateExplanations {
			exWriter.Flush()
		}
		if config.GenerateExampleSentences {
			esWriter.Flush()
		}

		log.Printf("\n- Category '%s' processed: %d words\n", category, len(sortedWords))
		fmt.Printf("\n- Category '%s' processed: %d words\n", category, len(sortedWords))
	}

	// Sort unknown words by frequency in descending order
	type UnknownWordFreq struct {
		Word  string
		Count int
	}

	var unknownWordsFreqList []UnknownWordFreq
	for word, count := range uniqueUnknownWords {
		if word != "" { // Skip empty words
			unknownWordsFreqList = append(unknownWordsFreqList, UnknownWordFreq{Word: word, Count: count})
		}
	}

	// Sort by frequency (highest first)
	sort.Slice(unknownWordsFreqList, func(i, j int) bool {
		return unknownWordsFreqList[i].Count > unknownWordsFreqList[j].Count
	})

	// Create UnknownWords.txt file with deduplicated content sorted by frequency
	unknownWordsFilePath := filepath.Join(outputDir, "UnknownWords.txt")
	unknownWordsFile, err := os.Create(unknownWordsFilePath)
	if err != nil {
		return fmt.Errorf("failed to create UnknownWords.txt file: %v", err)
	}
	defer unknownWordsFile.Close()
	unknownWordsWriter := bufio.NewWriter(unknownWordsFile)

	// Write unknown words sorted by frequency
	for _, wordFreq := range unknownWordsFreqList {
		unknownWordsWriter.WriteString(capitalizePhrase(wordFreq.Word) + "\n")
		unknownWordsWriter.WriteString(capitalizePhrase(wordFreq.Word) + "\n")
	}

	// Flush the unknown words file
	unknownWordsWriter.Flush()
	log.Println("- UnknownWords.txt complete (deduplicated and sorted by frequency)")
	fmt.Println("- UnknownWords.txt complete (deduplicated and sorted by frequency)")

	log.Println("\nGenerating final outputs...")
	fmt.Println("\nGenerating final outputs...")

	// Track known and unknown words separately
	var knownWords []string

	// Separate known and unknown words
	for _, word := range sortedAllWords {
		// Check if word is in our uniqueUnknownWords map
		if _, isUnknown := uniqueUnknownWords[strings.ToLower(word)]; !isUnknown {
			knownWords = append(knownWords, word)
		}
	}

	// Write `_AllWords.txt` file (always created, but only with known words)
	allWordsFilePath := filepath.Join(outputDir, baseFileName+"_AllWords.txt")
	allWordsFile, err := os.Create(allWordsFilePath)
	if err != nil {
		return fmt.Errorf("failed to create _AllWords.txt file: %v", err)
	}
	defer allWordsFile.Close()

	allWordsWriter := bufio.NewWriter(allWordsFile)
	for _, word := range knownWords {
		allWordsWriter.WriteString(capitalizePhrase(word) + "\n")
	}
	allWordsWriter.Flush()
	log.Println("- AllWords.txt complete")
	fmt.Println("- AllWords.txt complete")

	// Only create AllWords_ex.txt if the toggle is enabled
	if config.GenerateExplanations {
		// Write `_AllWords_ex.txt` file
		allWordsExFilePath := filepath.Join(outputDir, baseFileName+"_AllWords_ex.txt")
		allWordsExFile, err := os.Create(allWordsExFilePath)
		if err != nil {
			return fmt.Errorf("failed to create _AllWords_ex.txt file: %v", err)
		}
		defer allWordsExFile.Close()

		allWordsExWriter := bufio.NewWriter(allWordsExFile)
		hasWrittenAllWordsExplanations := false

		for i, word := range knownWords {
			printProgress("Processing All Words explanations", word, i+1, len(knownWords))
			wordDetailsText := fetchWordDetails(word)
			if wordDetailsText != "" {
				if hasWrittenAllWordsExplanations {
					allWordsExWriter.WriteString("\n" + wordDetailsText)
				} else {
					allWordsExWriter.WriteString(wordDetailsText)
					hasWrittenAllWordsExplanations = true
				}
			}
		}
		allWordsExWriter.Flush()
		log.Println("\n- AllWords_ex.txt complete")
		fmt.Println("\n- AllWords_ex.txt complete")
	}

	// Only create AllWords_es.txt if the toggle is enabled
	if config.GenerateExampleSentences {
		// Write `_AllWords_es.txt` file
		allWordsEsFilePath := filepath.Join(outputDir, baseFileName+"_AllWords_es.txt")
		allWordsEsFile, err := os.Create(allWordsEsFilePath)
		if err != nil {
			return fmt.Errorf("failed to create _AllWords_es.txt file: %v", err)
		}
		defer allWordsEsFile.Close()

		allWordsEsWriter := bufio.NewWriter(allWordsEsFile)
		hasWrittenAllWordsExamples := false

		for i, word := range knownWords {
			printProgress("Processing All Words example sentences", word, i+1, len(knownWords))
			exampleContent := generateExampleSentencesContent(word)
			if exampleContent != "" {
				if hasWrittenAllWordsExamples {
					allWordsEsWriter.WriteString("\n" + exampleContent)
				} else {
					allWordsEsWriter.WriteString(exampleContent)
					hasWrittenAllWordsExamples = true
				}
			}
		}
		allWordsEsWriter.Flush()
		log.Println("\n- AllWords_es.txt complete")
		fmt.Println("\n- AllWords_es.txt complete")
	}

	// Report results
	unknownCount := len(uniqueUnknownWords)
	knownCount := len(knownWords)

	log.Printf("\n===== Analysis Results =====\n")
	log.Printf("Total unique words after deduplication: %d\n", totalUniqueWords)
	log.Printf("Known words: %d, Unknown words: %d\n", knownCount, unknownCount)
	log.Printf("Results written to directory: %s\n", outputDir)
	if config.GenerateExplanations {
		log.Printf("Word explanation files were generated.\n")
	} else {
		log.Printf("Word explanation files were not generated (disabled in config).\n")
	}
	if config.GenerateExampleSentences {
		log.Printf("Example sentences files were generated.\n")
		if config.MaxExampleSentences > 0 {
			log.Printf("Example sentences were limited to a maximum of %d per word.\n", config.MaxExampleSentences)
		} else {
			log.Printf("No limit was applied to the number of example sentences per word.\n")
		}
	} else {
		log.Printf("Example sentences files were not generated (disabled in config).\n")
	}

	fmt.Printf("\n===== Analysis Results =====\n")
	fmt.Printf("Total unique words after deduplication: %d\n", totalUniqueWords)
	fmt.Printf("Known words: %d, Unknown words: %d\n", knownCount, unknownCount)
	fmt.Printf("Results written to directory: %s\n", outputDir)
	if config.GenerateExplanations {
		fmt.Printf("Word explanation files were generated.\n")
	} else {
		fmt.Printf("Word explanation files were not generated (disabled in config).\n")
	}
	if config.GenerateExampleSentences {
		fmt.Printf("Example sentences files were generated.\n")
		if config.MaxExampleSentences > 0 {
			fmt.Printf("Example sentences were limited to a maximum of %d per word.\n", config.MaxExampleSentences)
		} else {
			fmt.Printf("No limit was applied to the number of example sentences per word.\n")
		}
	} else {
		fmt.Printf("Example sentences files were not generated (disabled in config).\n")
	}
	log.Println("Text analysis complete.")

	return nil
}

func main() {
	// Initialize random number generator with current time as seed
	rand.Seed(time.Now().UnixNano())

	// Setup logging
	setupLogging()
	defer logFile.Close()

	log.Println("Application started")

	// Load configuration and proxy settings
	config = loadConfig()
	queryConfig = loadQueryConfig()
	proxyConfig = loadProxyConfig()
	loadWordCache()
	loadWordUnknown()

	// Load input configuration
	inputConfig := loadInputConfig()

	var inputFile string
	var err error

	// Check if the input file path is configured and valid
	if inputConfig.FilePath != "" {
		// Check if the file exists and is readable
		if _, err := os.Stat(inputConfig.FilePath); err == nil {
			log.Println("Using configured input file:", inputConfig.FilePath)
			fmt.Println("Using configured input file:", inputConfig.FilePath)
			inputFile = inputConfig.FilePath
		} else {
			log.Println("Configured input file not found or not accessible:", inputConfig.FilePath)
			fmt.Println("Configured input file not found or not accessible:", inputConfig.FilePath)
			log.Println("Falling back to file selection dialog")
			fmt.Println("Falling back to file selection dialog")

			// Fall back to GUI selection
			inputFile, err = dialog.File().Title("Select Input File").Filter("Text Files (*.txt)", "txt").Load()
			if err != nil || inputFile == "" {
				log.Println("No file selected or error occurred.")
				fmt.Println("No file selected or error occurred.")
				return
			}
		}
	} else {
		// No input file configured, use GUI selection as before
		log.Println("Select the input text file:")
		fmt.Println("Select the input text file:")
		inputFile, err = dialog.File().Title("Select Input File").Filter("Text Files (*.txt)", "txt").Load()
		if err != nil || inputFile == "" {
			log.Println("No file selected or error occurred.")
			fmt.Println("No file selected or error occurred.")
			return
		}
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
