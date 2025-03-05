package main

import (
	"fmt"
	"github.com/researchaccelerator-hub/telegram-scraper/common"
	"github.com/researchaccelerator-hub/telegram-scraper/dapr"
	"github.com/researchaccelerator-hub/telegram-scraper/standalone"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile      string
	crawlerCfg   common.CrawlerConfig
	urlList      []string
	urlFile      string
	generateCode bool
	crawlType    string
	minPostDate  string
)

func main() {
	// Initialize and execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// Root command setup
var rootCmd = &cobra.Command{
	Use:   "crawler",
	Short: "A flexible web crawler that can run as a DAPR job or standalone",
	Long: `A web crawler application that can run in two modes:
1. As a DAPR job - waiting for job requests
2. As a standalone application - processing URLs directly provided`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Load configuration file if specified
		if cfgFile != "" {
			viper.SetConfigFile(cfgFile)
		} else {
			// Search for config in default locations
			viper.AddConfigPath(".")
			viper.AddConfigPath("$HOME/.crawler")
			viper.AddConfigPath("/etc/crawler")
			viper.SetConfigName("config")
			viper.SetConfigType("yaml")
		}

		// Read environment variables prefixed with CRAWLER_
		viper.SetEnvPrefix("CRAWLER")
		viper.AutomaticEnv()
		viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))

		// Load the configuration
		if err := viper.ReadInConfig(); err != nil {
			// It's okay if there is no config file
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return err
			}
		}

		// Bind configuration to structure
		crawlerCfg.DaprMode = viper.GetBool("dapr.enabled")
		crawlerCfg.DaprPort = viper.GetInt("dapr.port")
		crawlerCfg.Concurrency = viper.GetInt("crawler.concurrency")
		crawlerCfg.Timeout = viper.GetInt("crawler.timeout")
		crawlerCfg.UserAgent = viper.GetString("crawler.useragent")
		crawlerCfg.OutputFormat = viper.GetString("output.format")
		crawlerCfg.StorageRoot = viper.GetString("storage.root")
		minPostDateStr := viper.GetString("crawler.minpostdate")
		if minPostDateStr != "" {
			parsedTime, err := time.Parse("2006-01-02", minPostDateStr)
			if err != nil {
				return fmt.Errorf("invalid min-post-date format, must be YYYY-MM-DD: %v", err)
			}
			crawlerCfg.MinPostDate = parsedTime
		} else {
			// Set to zero time if not specified
			crawlerCfg.MinPostDate = time.Time{}
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		// If no specific subcommand is invoked, show help
		if len(args) == 0 && !crawlerCfg.DaprMode && len(urlList) == 0 && urlFile == "" {
			cmd.Help()
			return
		}

		// Start in appropriate mode
		if crawlerCfg.DaprMode {
			dapr.StartDaprMode(crawlerCfg)
		} else {
			standalone.StartStandaloneMode(urlList, urlFile, crawlerCfg, generateCode)
		}
	},
}

// Initialize cobra command
func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&crawlerCfg.DaprMode, "dapr", false, "run as a DAPR job")
	rootCmd.PersistentFlags().IntVar(&crawlerCfg.DaprPort, "dapr-port", 6400, "DAPR port to use")
	rootCmd.PersistentFlags().IntVar(&crawlerCfg.Concurrency, "concurrency", 1, "number of concurrent crawlers")
	rootCmd.PersistentFlags().IntVar(&crawlerCfg.Timeout, "timeout", 30, "HTTP request timeout in seconds")
	rootCmd.PersistentFlags().StringVar(&crawlerCfg.UserAgent, "user-agent", "Mozilla/5.0 Crawler", "User agent to use")
	rootCmd.PersistentFlags().StringVar(&crawlerCfg.OutputFormat, "output", "json", "Output format (json, csv, etc.)")
	rootCmd.PersistentFlags().StringVar(&crawlerCfg.StorageRoot, "storage-root", "/tmp", "Storage root directory")
	rootCmd.PersistentFlags().StringVar(&minPostDate, "min-post-date", "", "Minimum post date to crawl (format: YYYY-MM-DD)")

	// Standalone mode specific flags
	rootCmd.Flags().StringSliceVar(&urlList, "urls", []string{}, "comma-separated list of URLs to crawl")
	rootCmd.Flags().StringVar(&urlFile, "url-file", "", "file containing URLs to crawl (one per line)")
	rootCmd.Flags().BoolVar(&generateCode, "generate-code", false, "run code generation after crawling")
	rootCmd.Flags().StringVar(&crawlType, "crawl-type", "focused", "Select between focused(default) and snowball")

	// Bind flags to viper
	viper.BindPFlag("dapr.enabled", rootCmd.PersistentFlags().Lookup("dapr"))
	viper.BindPFlag("dapr.port", rootCmd.PersistentFlags().Lookup("dapr-port"))
	viper.BindPFlag("crawler.concurrency", rootCmd.PersistentFlags().Lookup("concurrency"))
	viper.BindPFlag("crawler.timeout", rootCmd.PersistentFlags().Lookup("timeout"))
	viper.BindPFlag("crawler.useragent", rootCmd.PersistentFlags().Lookup("user-agent"))
	viper.BindPFlag("output.format", rootCmd.PersistentFlags().Lookup("output"))
	viper.BindPFlag("storage.root", rootCmd.PersistentFlags().Lookup("storage-root"))
	viper.BindPFlag("crawler.minpostdate", rootCmd.PersistentFlags().Lookup("min-post-date"))
	// Add subcommands
	rootCmd.AddCommand(versionCmd)
}

// Version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Crawler v1.0")
	},
}
