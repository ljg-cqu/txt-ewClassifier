/*
   TextLinguisticAnalyzer: A comprehensive Go program for linguistic analysis with local dictionary caching.

   Description:
   This program analyzes text files, categorizes words by parts of speech, and provides
   detailed word explanations using the Free Dictionary API. It implements a local
   JSON-based cache to improve performance and reduce API calls.

   Features:
   - Categorizes words by linguistic function (nouns, verbs, adjectives, adverbs)
   - Provides detailed word explanations from Free Dictionary API
   - Stores word definitions in a local JSON file (LocalFreeDictionary.json)
   - Queries local cache before making remote API requests
   - Handles slash-separated words as separate entries
   - Generates multiple output files for different word categories
   - Formats word definitions in a readable, numbered format
   - Tracks query progress with clear status updates
   - Organizes output in separate text files with proper formatting

   Workflow:
   1. User selects an input text file via GUI dialog
   2. Program classifies words by part of speech using NLP
   3. For each word requiring an explanation:
      a. First checks local cache for existing definitions
      b. If not found locally, queries the Free Dictionary API
      c. Stores new definitions in the local cache
   4. Outputs categorized words and explanations to files
   5. Generates comprehensive "_AllWords.txt" and "_AllWords_ex.txt" files
   6. Displays completion confirmation dialog
*/

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
	"sync"
	"unicode"

	"github.com/jdkato/prose/v2"
	"github.com/sqweek/dialog"
)

const (
	// The filename for the local dictionary cache
	DictionaryCacheFile = "LocalFreeDictionary.json"
)

// Structure to hold word definitions
type WordDefinition struct {
	Word          string `json:"word"`
	DefinitionNum int    `json:"definition_number"`
	PartOfSpeech  string `json:"part_of_speech"`
	Definition    string `json:"definition"`
	Example       string `json:"example,omitempty"`
}

// Structure to hold the dictionary cache
type DictionaryCache struct {
	Definitions map[string][]WordDefinition `json:"definitions"`
	mutex       sync.RWMutex
}

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

// Initialize the dictionary cache
func initDictionaryCache() (*DictionaryCache, error) {
	cache := &DictionaryCache{
		Definitions: make(map[string][]WordDefinition),
	}

	// Check if the cache file exists
	if _, err := os.Stat(DictionaryCacheFile); os.IsNotExist(err) {
		// File doesn't exist, return empty cache
		return cache, nil
	}

	// Read and parse the cache file
	data, err := ioutil.ReadFile(DictionaryCacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %v", err)
	}

	if err := json.Unmarshal(data, &cache.Definitions); err != nil {
		return nil, fmt.Errorf("failed to parse cache file: %v", err)
	}

	return cache, nil
}

// Save the dictionary cache to disk
func (cache *DictionaryCache) Save() error {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	// Marshal the definitions to JSON
	data, err := json.MarshalIndent(cache.Definitions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %v", err)
	}

	// Write to the cache file
	if err := ioutil.WriteFile(DictionaryCacheFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %v", err)
	}

	return nil
}

// Get word definitions from cache
func (cache *DictionaryCache) GetDefinitions(word string) []WordDefinition {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	lowerWord := strings.ToLower(word)
	return cache.Definitions[lowerWord]
}

// Add a word definition to the cache
func (cache *DictionaryCache) AddDefinition(def WordDefinition) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	lowerWord := strings.ToLower(def.Word)
	cache.Definitions[lowerWord] = append(cache.Definitions[lowerWord], def)
}

// Format definitions for a word from the cache
func formatCachedDefinitions(word string, defs []WordDefinition) string {
	var sb strings.Builder
	sb.WriteString(capitalizePhrase(word) + "\n")

	// Sort definitions by definition number
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].DefinitionNum < defs[j].DefinitionNum
	})

	for _, def := range defs {
		sb.WriteString(fmt.Sprintf("\t%s %d, %s: %s\n",
			capitalizePhrase(word), def.DefinitionNum, def.PartOfSpeech, def.Definition))
		if def.Example != "" {
			sb.WriteString(fmt.Sprintf("\t\t%s %d Example: %s\n",
				capitalizePhrase(word), def.DefinitionNum, def.Example))
		}
	}

	return sb.String()
}

// Fetch word details from local cache or Free Dictionary API
func fetchWordDetails(cache *DictionaryCache, word string) string {
	// First check if we have this word in the local cache
	cachedDefs := cache.GetDefinitions(word)
	if len(cachedDefs) > 0 {
		return formatCachedDefinitions(word, cachedDefs)
	}

	// If not found locally, query the Free Dictionary API
	url := fmt.Sprintf("https://api.dictionaryapi.dev/api/v2/entries/en/%s", word)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
	}
	defer resp.Body.Close()

	var result []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
	}

	var details strings.Builder
	// Start with the word name on its own line
	details.WriteString(capitalizePhrase(word) + "\n")

	wordNumber := 1
	for _, entry := range result {
		meanings, ok := entry["meanings"].([]interface{})
		if !ok {
			continue
		}

		for _, meaning := range meanings {
			meaningMap, ok := meaning.(map[string]interface{})
			if !ok {
				continue
			}

			partOfSpeech, ok := meaningMap["partOfSpeech"].(string)
			if !ok {
				continue
			}

			definitions, ok := meaningMap["definitions"].([]interface{})
			if !ok {
				continue
			}

			for _, definition := range definitions {
				defMap, ok := definition.(map[string]interface{})
				if !ok {
					continue
				}

				definitionText, ok := defMap["definition"].(string)
				if !ok {
					continue
				}

				// Write to details string builder
				details.WriteString(fmt.Sprintf("\t%s %d, %s: %s\n", capitalizePhrase(word), wordNumber, partOfSpeech, definitionText))

				// Check for example
				example, hasExample := defMap["example"].(string)
				if hasExample {
					details.WriteString(fmt.Sprintf("\t\t%s %d Example: %s\n", capitalizePhrase(word), wordNumber, example))
				} else {
					example = "" // Set empty string for storage
				}

				// Store in local cache
				cache.AddDefinition(WordDefinition{
					Word:          word,
					DefinitionNum: wordNumber,
					PartOfSpeech:  partOfSpeech,
					Definition:    definitionText,
					Example:       example,
				})

				wordNumber++
			}
		}
	}

	// If no definitions were found
	if wordNumber == 1 {
		return fmt.Sprintf("%s\n\tNo details available.\n", capitalizePhrase(word))
	}

	return details.String()
}

// Prints dynamic progress monitoring info with clear formatting
func printProgress(stage string, item string, current, total int) {
	percentage := int((float64(current) / float64(total)) * 100)
	// Clear the entire line before printing new progress
	fmt.Printf("\r%-80s", " ") // Clear previous content with spaces
	fmt.Printf("\r%s: %s (%d of %d words) - %d%% Complete", stage, capitalizePhrase(item), current, total, percentage)
}

// Categorizes text based on linguistic features
func categorizeText(inputFile string) error {
	// Initialize the dictionary cache
	cache, err := initDictionaryCache()
	if err != nil {
		return fmt.Errorf("cache initialization error: %v", err)
	}
	// Save cache when we're done
	defer cache.Save()

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

	fmt.Println("\nClassification complete.")
	fmt.Println("Starting word dictionary lookups...")

	// Get all unique words for total word count display
	sortedAllWords := sortByFrequency(allWords)
	totalUniqueWords := len(sortedAllWords)

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

		for idx, word := range sortedWords {
			wordWriter.WriteString(capitalizePhrase(word) + "\n")
			printProgress(
				fmt.Sprintf("Dictionary lookup (%s)", category),
				word,
				idx+1,
				len(sortedWords))
			exWriter.WriteString(fetchWordDetails(cache, word) + "\n")
		}
		wordWriter.Flush()
		exWriter.Flush()
		fmt.Printf("\n- Category '%s': %d words processed\n", category, len(sortedWords))
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
	for idx, word := range sortedAllWords {
		printProgress("Creating AllWords_ex.txt", word, idx+1, totalUniqueWords)
		allWordsExWriter.WriteString(fetchWordDetails(cache, word) + "\n")
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
