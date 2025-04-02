package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
var wordCache map[string]WordCache = make(map[string]WordCache)
var cachePath string = "word_cache.json"

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
			fmt.Println("Error creating default config:", err)
			return defaultConfig
		}
		err = ioutil.WriteFile(configPath, yamlData, 0644)
		if err != nil {
			fmt.Println("Error writing default config file:", err)
		}
		return defaultConfig
	}

	// Read and parse config file
	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		fmt.Printf("Error reading config file: %v. Using defaults.\n", err)
		return defaultConfig
	}

	var config OutputConfig
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		fmt.Printf("Error parsing config file: %v. Using defaults.\n", err)
		return defaultConfig
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
		fmt.Println("Error reading cache file:", err)
		return
	}

	err = json.Unmarshal(data, &wordCache)
	if err != nil {
		fmt.Println("Error parsing cache file:", err)
		wordCache = make(map[string]WordCache)
	}
}

// Save word cache to file
func saveWordCache() {
	data, err := json.MarshalIndent(wordCache, "", "  ")
	if err != nil {
		fmt.Println("Error serializing cache:", err)
		return
	}

	err = ioutil.WriteFile(cachePath, data, 0644)
	if err != nil {
		fmt.Println("Error writing cache file:", err)
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

// Fetch word explanations using the Free Dictionary API
func fetchWordDetails(word string) string {
	// Check cache first
	cachedData, exists := checkCache(word)
	if !exists {
		// Fetch from API if not in cache
		url := fmt.Sprintf("https://api.dictionaryapi.dev/api/v2/entries/en/%s", strings.ToLower(word))
		resp, err := http.Get(url)
		if err != nil {
			return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
		}
		defer resp.Body.Close()

		var result []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
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
		if len(result) > 0 {
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
		}

		// Process all definitions
		for _, entry := range result {
			if meanings, ok := entry["meanings"].([]interface{}); ok {
				for _, meaning := range meanings {
					meaningMap := meaning.(map[string]interface{})
					partOfSpeech := meaningMap["partOfSpeech"].(string)

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
							defMap := def.(map[string]interface{})
							definitionText := defMap["definition"].(string)

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

// Prints dynamic progress monitoring info with clear formatting
func printProgress(stage string, item string, current, total int) {
	percentage := int((float64(current) / float64(total)) * 100)
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
	fmt.Println("Starting text classification...")

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
		fmt.Printf("\n- Category '%s' processed: %d words\n", category, len(sortedWords))
	}

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
	fmt.Println("- AllWords.txt complete")

	// Report results
	fmt.Printf("\n===== Analysis Results =====\n")
	fmt.Printf("Total unique words after deduplication: %d\n", totalUniqueWords)
	fmt.Printf("Results written to directory: %s\n", outputDir)

	return nil
}

func main() {
	// Load configuration and word cache
	config = loadConfig()
	loadWordCache()

	fmt.Println("Select the input text file:")
	inputFile, err := dialog.File().Title("Select Input File").Filter("Text Files (*.txt)", "txt").Load()
	if err != nil || inputFile == "" {
		fmt.Println("No file selected or error occurred.")
		return
	}

	err = categorizeText(inputFile)
	if err != nil {
		fmt.Println("Error during categorization:", err)
		return
	}

	// Display confirmation dialog for completion
	dialog.Message("Text analysis complete. Click 'OK' to exit.").Title("Analysis Results").Info()
}
