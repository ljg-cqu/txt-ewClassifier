package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/jdkato/prose/v2"
	"github.com/sqweek/dialog"
)

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
		words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
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

// Fetch word details using the Free Dictionary API
func fetchWordDetails(word string) string {
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
	details.WriteString(capitalizePhrase(word) + "\n")
	for _, entry := range result {
		meanings := entry["meanings"].([]interface{})
		for _, meaning := range meanings {
			meaningMap := meaning.(map[string]interface{})
			partOfSpeech := meaningMap["partOfSpeech"].(string)
			definitions := meaningMap["definitions"].([]interface{})
			for _, definition := range definitions {
				defMap := definition.(map[string]interface{})
				def := defMap["definition"].(string)
				example, exampleExists := defMap["example"].(string)
				details.WriteString(fmt.Sprintf("\t(%s): %s\n", partOfSpeech, def))
				if exampleExists {
					details.WriteString(fmt.Sprintf("\t\tExample: %s\n", example))
				}
			}
		}
	}
	return details.String()
}

// Prints dynamic progress monitoring info
func printProgress(word string, wordsQueried, totalWords int) {
	progress := int((float64(wordsQueried) / float64(totalWords)) * 100)
	fmt.Printf("\rQuery Progress: Word [%s], %d/%d completed (%d%%)", capitalizePhrase(word), wordsQueried, totalWords, progress)
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

	explanationFiles := make(map[string]string)
	for category, file := range categoryFiles {
		explanationFiles[category] = strings.Replace(file, ".txt", "_ex.txt", 1)
	}

	categorizedContent := make(map[string][]string)
	allWords := make(map[string]int)

	// Process tokens for classification
	tokens := doc.Tokens()
	totalTokens := len(tokens)
	for index, tok := range tokens {
		text := strings.ToLower(tok.Text)
		printProgress(text, index+1, totalTokens)

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

	// Write categorized content and explanations to respective files
	for category, words := range categorizedContent {
		wordFilePath := filepath.Join(outputDir, categoryFiles[category])
		explanationFilePath := filepath.Join(outputDir, explanationFiles[category])

		wordFile, err := os.Create(wordFilePath)
		if err != nil {
			return fmt.Errorf("failed to create output file for %s: %v", category, err)
		}
		defer wordFile.Close()

		explanationFile, err := os.Create(explanationFilePath)
		if err != nil {
			return fmt.Errorf("failed to create explanation file for %s: %v", category, err)
		}
		defer explanationFile.Close()

		wordWriter := bufio.NewWriter(wordFile)
		explanationWriter := bufio.NewWriter(explanationFile)

		sortedWords := sortByFrequency(countFrequencies(words))
		for _, word := range sortedWords {
			wordWriter.WriteString(capitalizePhrase(word) + "\n")
			explanationWriter.WriteString(fetchWordDetails(word) + "\n")
		}
		wordWriter.Flush()
		explanationWriter.Flush()
	}

	// Write `_AllWords_ex.txt` file
	allWordsExFilePath := filepath.Join(outputDir, baseFileName+"_AllWords_ex.txt")
	allWordsExFile, err := os.Create(allWordsExFilePath)
	if err != nil {
		return fmt.Errorf("failed to create _AllWords_ex.txt file: %v", err)
	}
	defer allWordsExFile.Close()

	allWordsExWriter := bufio.NewWriter(allWordsExFile)
	sortedAllWords := sortByFrequency(allWords)
	for _, word := range sortedAllWords {
		allWordsExWriter.WriteString(fetchWordDetails(word) + "\n")
	}
	allWordsExWriter.Flush()

	// Write `_AllWords.txt` file
	allWordsPlainFilePath := filepath.Join(outputDir, baseFileName+"_AllWords.txt")
	allWordsPlainFile, err := os.Create(allWordsPlainFilePath)
	if err != nil {
		return fmt.Errorf("failed to create _AllWords.txt file: %v", err)
	}
	defer allWordsPlainFile.Close()

	allWordsPlainWriter := bufio.NewWriter(allWordsPlainFile)
	for _, word := range sortedAllWords {
		allWordsPlainWriter.WriteString(capitalizePhrase(word) + "\n")
	}
	allWordsPlainWriter.Flush()

	// Report results
	fmt.Printf("\n\n===== Analysis Results =====\n")
	fmt.Printf("Total unique words after deduplication: %d\n", len(allWords))
	fmt.Printf("Results written to directory: %s\n", outputDir)

	return nil
}

// Main function
func main() {
	inputFile, err := dialog.File().Title("Select Input Text File").Filter("Text Files", "txt").Load()
	if err != nil || inputFile == "" {
		fmt.Println("No file selected or error occurred:", err)
		return
	}

	err = categorizeText(inputFile)
	if err != nil {
		fmt.Println("Error during categorization:", err)
		return
	}

	dialog.Message("Text analysis complete. Click OK to exit.").Title("Completion").Info()
}
