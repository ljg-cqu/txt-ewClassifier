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

// Configuration structures
type OutputConfig struct {
	IncludePhonetic bool `yaml:"includePhonetic"`
	IncludeOrigin   bool `yaml:"includeOrigin"`
	IncludeSynonyms bool `yaml:"includeSynonyms"`
	IncludeAntonyms bool `yaml:"includeAntonyms"`
	FilterNoExample bool `yaml:"filterDefinitionsWithoutExamples"`
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
var proxyConfig ProxyConfig
var wordCache = make(map[string]WordCache)
var cachePath = "word_cache.json"
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
func loadConfig() OutputConfig {
	defaultConfig := OutputConfig{
		IncludePhonetic: false,
		IncludeOrigin:   false,
		IncludeSynonyms: false,
		IncludeAntonyms: false,
		FilterNoExample: false,
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

func fetchWordDetails(word string) string {
	word = strings.ToLower(word)
	cachedData, exists := wordCache[word]

	if !exists {
		apiURL := fmt.Sprintf("https://api.dictionaryapi.dev/api/v2/entries/en/%s", word)

		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
		}

		req.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Add("Accept", "application/json")

		client := createHTTPClient()
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
		}
		defer resp.Body.Close()

		bodyBytes, _ := ioutil.ReadAll(resp.Body)

		var result []map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil || len(result) == 0 {
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

		// Save to cache
		wordCache[strings.ToLower(word)] = cachedData
		saveWordCache()
	}

	// Format output with the new layout
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
		output.WriteString(fmt.Sprintf("\t%s: No details available.\n", capitalized))
		return output.String()
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
	for category, file := range categories {
		explanationFiles[category] = strings.Replace(file, ".txt", "_ex.txt", 1)
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

	// Write categorized content to individual files
	for category, words := range categorizedWords {
		filePath := categories[category]
		exFilePath := explanationFiles[category]

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
